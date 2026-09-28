package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDelimitedInput: a flow with inputFormat delimited reads CSV files from
// a file source, transforms the rows, and delivers JSON; the options apply
// and are refused on other formats.
func TestDelimitedInput(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	in, out := t.TempDir(), t.TempDir()
	createFlow(t, c, `{"id":"roster","inputFormat":"delimited","delimited":{"delimiter":";"},`+
		`"source":{"type":"file","dir":"`+in+`","pattern":"*.csv","pollIntervalMs":100},`+
		`"transform":{"name":"t","steps":[{"map":{"from":"rows.0.lastName","to":"first.last"}},{"map":{"from":"rows.1.note","to":"second.note"}}]},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	csv := "mrn;lastName;note\r\n123;DOE;plain\r\n456;ROE;\"has; a delimiter\"\r\n"
	p := filepath.Join(in, "roster.csv")
	if err := os.WriteFile(p, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(p, old, old)
	deadline := time.Now().Add(10 * time.Second)
	var entries []os.DirEntry
	for len(entries) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the CSV file was never delivered")
		}
		time.Sleep(20 * time.Millisecond)
		entries, _ = os.ReadDir(out)
	}
	got, _ := os.ReadFile(filepath.Join(out, entries[0].Name()))
	for _, want := range []string{`"first":{"last":"DOE"}`, `"second":{"note":"has; a delimiter"}`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("delivered %s, missing %s", got, want)
		}
	}

	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/roster/messages", "a;b\n1\n", admin); code != http.StatusBadRequest || !strings.Contains(body, "rows have different numbers of fields") {
		t.Errorf("ragged rows: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","inputFormat":"json","delimited":{"delimiter":";"}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "delimited applies only to inputFormat delimited") {
		t.Errorf("delimited on a json flow: %d %s", code, body)
	}
}
