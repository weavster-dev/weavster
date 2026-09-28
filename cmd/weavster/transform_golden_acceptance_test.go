package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// goldenExt is the expected-output file extension per output format, and
// goldenType the Content-Type the destination must receive it with.
var (
	goldenExt  = map[string]string{"json": ".json", "hl7v2": ".hl7", "xml": ".xml", "text": ".txt"}
	goldenType = map[string]string{"json": "application/json", "hl7v2": "x-application/hl7-v2+er7", "xml": "application/xml", "text": "text/plain; charset=utf-8"}
)

// TestTransformGolden runs every case under testdata/transforms through
// the real server. A case has flow.json (a flow definition without id or
// destinations; its inputFormat says how input.* is read), one input.*
// file, and either expected.<ext> (the output, <ext> named by the output
// format: the transform's build format, else json) or expected.status
// holding "filtered". The input is sent with the API to an http
// destination, whose body and Content-Type are checked. Run with -update
// to rewrite expected.<ext> (stale expected.* files are removed).
func TestTransformGolden(t *testing.T) {
	cases, err := filepath.Glob("testdata/transforms/*")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no golden cases: %v", err)
	}
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
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, dir := range cases {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			mu.Lock()
			bodies, types = nil, nil
			mu.Unlock()
			raw, err := os.ReadFile(filepath.Join(dir, "flow.json"))
			if err != nil {
				t.Fatal(err)
			}
			var flow map[string]any
			if err := json.Unmarshal(raw, &flow); err != nil {
				t.Fatalf("flow.json: %v", err)
			}
			if flow["id"] != nil || flow["destinations"] != nil {
				t.Fatal("flow.json must not set id or destinations; the test adds them")
			}
			flow["id"] = name
			flow["destinations"] = []any{map[string]any{"name": "out", "type": "http", "url": receiver.URL}}
			def, _ := json.Marshal(flow) // a decoded document always encodes
			createFlow(t, c, string(def))

			inputs, err := filepath.Glob(filepath.Join(dir, "input.*"))
			if err != nil || len(inputs) != 1 {
				t.Fatalf("want one input.* file, got %v (%v)", inputs, err)
			}
			input, err := os.ReadFile(inputs[0])
			if err != nil {
				t.Fatal(err)
			}
			code, body, _ := c.do(http.MethodPost, "/api/v1/flows/"+name+"/messages", string(input), admin)
			var res struct{ Status string }
			if err := json.Unmarshal([]byte(body), &res); err != nil || code != http.StatusAccepted {
				t.Fatalf("send: %d %s (%v)", code, body, err)
			}
			mu.Lock()
			got, gotTypes := bodies, types
			mu.Unlock()

			if status, err := os.ReadFile(filepath.Join(dir, "expected.status")); err == nil {
				if want := strings.TrimSpace(string(status)); res.Status != want || len(got) != 0 {
					t.Errorf("status %s with %d deliveries, want %s and none", res.Status, len(got), want)
				}
				return
			}
			if res.Status != "sent" || len(got) != 1 {
				t.Fatalf("status %s with %d deliveries, want sent and one", res.Status, len(got))
			}
			format := buildFormat(flow)
			if gotTypes[0] != goldenType[format] {
				t.Errorf("Content-Type %q, want %q", gotTypes[0], goldenType[format])
			}
			want := filepath.Join(dir, "expected"+goldenExt[format])
			if *updateGolden {
				stale, _ := filepath.Glob(filepath.Join(dir, "expected.*"))
				for _, f := range stale {
					if err := os.Remove(f); err != nil {
						t.Fatal(err)
					}
				}
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
