package main

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateTransforms = flag.Bool("update-transforms", false, "rewrite testdata/transforms/*/expected.* from the server's output")

// TestTransformGolden runs every case under testdata/transforms through
// the real server: the case's flow definition (with a file destination
// added), its input sent with the API, and the delivered file compared
// byte for byte with expected.* (the input's own format is named by its
// extension; expected.<ext> by the output's). Run with -update-transforms
// to rewrite the expectations.
func TestTransformGolden(t *testing.T) {
	cases, err := filepath.Glob("testdata/transforms/*")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no golden cases: %v", err)
	}
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for _, dir := range cases {
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			def, err := os.ReadFile(filepath.Join(dir, "flow.json"))
			if err != nil {
				t.Fatal(err)
			}
			inputs, _ := filepath.Glob(filepath.Join(dir, "input.*"))
			if len(inputs) != 1 {
				t.Fatalf("want one input.* file, got %v", inputs)
			}
			input, _ := os.ReadFile(inputs[0])
			out := t.TempDir()
			flow := strings.TrimSuffix(strings.TrimSpace(string(def)), "}") +
				`,"id":"` + name + `","destinations":[{"name":"out","type":"file","dir":"` + out + `"}]}`
			createFlow(t, c, flow)
			if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/"+name+"/messages", string(input), admin); code != http.StatusAccepted || !strings.Contains(body, `"status":"sent"`) {
				t.Fatalf("send: %d %s", code, body)
			}
			delivered, _ := os.ReadDir(out)
			if len(delivered) != 1 {
				t.Fatalf("delivered %d files", len(delivered))
			}
			got, _ := os.ReadFile(filepath.Join(out, delivered[0].Name()))
			ext := map[string]string{"json": ".json", "hl7v2": ".hl7", "xml": ".xml", "text": ".txt"}[outputFormat(string(def))]
			want := filepath.Join(dir, "expected"+ext)
			if *updateTransforms {
				if err := os.WriteFile(want, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(want)
			if err != nil {
				t.Fatalf("%v (run with -update-transforms)", err)
			}
			if string(got) != string(expected) {
				t.Errorf("output differs from %s\ngot:  %q\nwant: %q", want, got, expected)
			}
		})
	}
}

// outputFormat is the format a case's flow outputs: its build step's, or
// json.
func outputFormat(def string) string {
	for _, f := range []string{"hl7v2", "xml", "text"} {
		if strings.Contains(def, `"format": "`+f+`"`) {
			return f
		}
	}
	return "json"
}
