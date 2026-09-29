package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// goldenType is the Content-Type a destination must receive each output
// format with.
var goldenType = map[string]string{"json": "application/json", "hl7v2": "x-application/hl7-v2+er7", "xml": "application/xml", "text": "text/plain; charset=utf-8"}

// goldenDir holds the golden cases, a directory each: an example
// repository that `weavster test examples/golden` also runs offline
// (TestGoldenFixtures). Its fixture file is the one statement of what the
// case expects, for both runs.
const goldenDir = "../../examples/golden"

// transformCase is a golden case directory: its weavster.json (a config-as-code
// document with just the case's flow, without destinations) and its one
// fixture file with one case.
type transformCase struct {
	dir     string
	fixture string // the fixture file
	flow    map[string]any
	test    fixtureCase
	name    string // the flow's id
}

// transformCases reads every case directory under goldenDir (other entries,
// such as a README, are left alone).
func transformCases(t *testing.T) []transformCase {
	t.Helper()
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal(err)
	}
	var cases []transformCase
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(goldenDir, e.Name())
		fixtures, _ := filepath.Glob(filepath.Join(dir, "*.test.yaml"))
		if len(fixtures) != 1 {
			t.Fatalf("%s: want one *.test.yaml, got %v", dir, fixtures)
		}
		raw, err := os.ReadFile(fixtures[0])
		if err != nil {
			t.Fatal(err)
		}
		var fx fixtureFile
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err := dec.Decode(&fx); err != nil || len(fx.Cases) != 1 {
			t.Fatalf("%s: want one case (%v)", fixtures[0], err)
		}
		raw, err = os.ReadFile(filepath.Join(dir, "weavster.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		var flows map[string]map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil || len(doc) != 2 || string(doc["version"]) != `"1"` || json.Unmarshal(doc["flows"], &flows) != nil || len(flows) != 1 {
			t.Fatalf("%s/weavster.json must be a version 1 document with only the case's flow (%v)", dir, err)
		}
		flow, ok := flows[fx.Flow]
		if !ok || flow["id"] != nil || flow["destinations"] != nil {
			t.Fatalf("%s/weavster.json: the flow must be %s, without id or destinations (the server test adds them)", dir, fx.Flow)
		}
		cases = append(cases, transformCase{dir: dir, fixture: fixtures[0], flow: flow, test: fx.Cases[0], name: fx.Flow})
	}
	if len(cases) == 0 {
		t.Fatal("no golden cases")
	}
	return cases
}

// TestTransformGolden runs every golden case through the real server: the
// case's input is sent with the API to its flow with an http destination
// added, and the delivered body and Content-Type are checked against the
// fixture's expectation (status filtered: nothing delivered; outputFile:
// the body). Run with -update to rewrite each case's outputFile from what
// the server delivered.
func TestTransformGolden(t *testing.T) {
	cases := transformCases(t)
	var mu sync.Mutex
	var bodies, types []string
	receiver := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies, types = append(bodies, string(b)), append(types, r.Header.Get("Content-Type"))
		mu.Unlock()
	}))
	defer receiver.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, gc := range cases {
		t.Run(gc.name, func(t *testing.T) {
			mu.Lock()
			bodies, types = nil, nil
			mu.Unlock()
			flow := gc.flow
			flow["id"] = gc.name
			flow["destinations"] = []any{map[string]any{"name": "out", "type": "http", "url": receiver.URL}}
			def, _ := json.Marshal(flow) // a decoded document always encodes
			createFlow(t, c, string(def))

			var input []byte
			if tc := gc.test; tc.Input != nil {
				input = []byte(*tc.Input)
			} else {
				b, err := os.ReadFile(filepath.Join(gc.dir, tc.InputFile))
				if err != nil {
					t.Fatal(err)
				}
				input = b
			}
			code, body, _ := c.do(http.MethodPost, "/api/v1/flows/"+gc.name+"/messages", string(input), admin)
			var res struct{ Status string }
			if err := json.Unmarshal([]byte(body), &res); err != nil || code != http.StatusAccepted {
				t.Fatalf("send: %d %s (%v)", code, body, err)
			}
			mu.Lock()
			got, gotTypes := bodies, types
			mu.Unlock()

			e := gc.test.Expect
			if e.Status == "filtered" {
				if res.Status != "filtered" || len(got) != 0 {
					t.Errorf("status %s with %d deliveries, want filtered and none", res.Status, len(got))
				}
				return
			}
			if e.Status != "" || e.OutputFile == "" {
				t.Fatalf("%s: a golden case expects status filtered, or an outputFile", gc.fixture)
			}
			if res.Status != "sent" || len(got) != 1 {
				t.Fatalf("status %s with %d deliveries, want sent and one", res.Status, len(got))
			}
			format := buildFormat(flow)
			if gotTypes[0] != goldenType[format] {
				t.Errorf("Content-Type %q, want %q", gotTypes[0], goldenType[format])
			}
			want := filepath.Join(gc.dir, e.OutputFile)
			if *updateGolden {
				if err := os.WriteFile(want, []byte(got[0]), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(want)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			if got[0] != string(expected) {
				t.Errorf("output differs from %s\ngot:  %q\nwant: %q", want, got[0], expected)
			}
		})
	}
}

// buildFormat is the output format of a flow definition: its transform's
// last step's build format, else json.
func buildFormat(flow map[string]any) string {
	tr, _ := flow["transform"].(map[string]any)
	steps, _ := tr["steps"].([]any)
	if len(steps) == 0 {
		return "json"
	}
	last, _ := steps[len(steps)-1].(map[string]any)
	build, ok := last["build"].(map[string]any)
	if !ok {
		return "json"
	}
	if format, _ := build["format"].(string); format != "" {
		return format
	}
	return "json"
}

// TestGoldenFixtures: `weavster test --format junit` runs the golden cases
// offline, as CI does, and every one passes, with the codec round trips.
func TestGoldenFixtures(t *testing.T) {
	cases := transformCases(t)
	out := t.TempDir()
	var stdout, stderr strings.Builder
	if code := run([]string{"test", "--format", "junit", "--output", out, goldenDir}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	junit, err := os.ReadFile(filepath.Join(out, "results.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(`tests="%d" failures="0"`, len(builtinFixtures())+len(cases)); !strings.Contains(string(junit), want) {
		t.Errorf("JUnit lacks %s:\n%s", want, junit)
	}
	for _, gc := range cases {
		name := fixtureName(gc.fixture) + "/" + gc.test.Name
		if !strings.Contains(string(junit), `name="`+name+`"`) {
			t.Errorf("no result for %s", name)
		}
	}
}
