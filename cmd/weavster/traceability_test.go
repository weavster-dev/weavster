package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestTraceabilityMatrix: specs/traceability-matrix.md (D-24) has a row for
// every MUST/SHALL line of the functional spec, quoting it as it is, and for
// every acceptance criterion of agentic-manifest.json; each row is tested,
// partial, or deferred, a partial or deferred row names its deferment (🔒 or
// a D-NN decision), and every test the matrix names exists.
func TestTraceabilityMatrix(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(path string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	matrix := read("specs/traceability-matrix.md")
	cell := func(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "|", `\|`) }
	rows := map[string]string{} // row prefix → the rest of the row
	for _, line := range strings.Split(matrix, "\n") {
		if m := regexp.MustCompile(`^\| ([LM][0-9.]+) \| `).FindStringSubmatch(line); m != nil {
			rows[m[1]] = line
		}
	}

	var want []string
	must := regexp.MustCompile(`\b(MUST|SHALL)\b`)
	for i, line := range strings.Split(read("specs/black-box-functional-spec.md"), "\n") {
		if !must.MatchString(line) || strings.Contains(line, "MUST / SHALL") { // the convention, not a requirement
			continue
		}
		id := fmt.Sprintf("L%d", i+1)
		want = append(want, id)
		if row, ok := rows[id]; !ok || !strings.HasPrefix(row, "| "+id+" | "+cell(line)+" | ") {
			t.Errorf("the matrix has no row %s quoting the spec's line %d as it is: %s", id, i+1, line)
		}
	}
	var manifest struct {
		Modules []struct {
			LogicalName        string   `json:"logical_name"`
			AcceptanceCriteria []string `json:"acceptance_criteria"`
		} `json:"modules"`
	}
	if err := json.Unmarshal([]byte(read("agentic-manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	for i, m := range manifest.Modules {
		for j, c := range m.AcceptanceCriteria {
			id := fmt.Sprintf("M%d.%d", i, j)
			want = append(want, id)
			if row, ok := rows[id]; !ok || !strings.HasPrefix(row, "| "+id+" | "+m.LogicalName+" | "+cell(c)+" | ") {
				t.Errorf("the matrix has no row %s quoting the manifest's criterion as it is: %s", id, c)
			}
		}
	}
	if len(want) < 100 || len(rows) != len(want) {
		t.Errorf("the matrix has %d rows, the spec and manifest %d requirements", len(rows), len(want))
	}

	status := regexp.MustCompile(` \| (tested|partial|deferred) \| (.*) \|$`)
	for _, id := range want {
		m := status.FindStringSubmatch(rows[id])
		switch {
		case m == nil:
			t.Errorf("row %s has no status (tested, partial, deferred)", id)
		case m[1] != "tested" && !regexp.MustCompile(`🔒|D-[0-9]+`).MatchString(m[2]):
			t.Errorf("row %s is %s but names no deferment (🔒 or D-NN)", id, m[1])
		}
	}

	var tests strings.Builder
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && (d.Name() == ".git" || d.Name() == "site") {
			return filepath.SkipDir
		}
		if err == nil && strings.HasSuffix(path, "_test.go") {
			b, rerr := os.ReadFile(path)
			tests.Write(b)
			return rerr
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	named := regexp.MustCompile("`(Test[A-Za-z0-9_]+)").FindAllStringSubmatch(matrix, -1)
	if len(named) < 100 {
		t.Fatalf("test names not read from the matrix: %d", len(named))
	}
	for _, n := range named {
		if !strings.Contains(tests.String(), "func "+n[1]+"(") {
			t.Errorf("the matrix names %s, which is not a test", n[1])
		}
	}
}
