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

// TestTraceabilityMatrix: specs/traceability-matrix.md (D-24) has exactly one
// row for every MUST/SHALL line of the functional spec, quoting it as it
// is, and for every acceptance criterion of agentic-manifest.json. Each row
// is tested (naming tests, or a CI job, and nothing deferred), partial, or
// deferred (naming the deferment: 🔒 or a D-NN decision), and every test
// the matrix names exists in the module's code.
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
	var (
		rowID    = regexp.MustCompile(`^\| ([LM][0-9.]+) \| `)
		status   = regexp.MustCompile(` \| (tested|partial|deferred) \| (.*) \|$`)
		deferral = regexp.MustCompile(`🔒|D-[0-9]+`)
		evidence = regexp.MustCompile("`Test[A-Za-z0-9_]+|CI `[a-z-]+` job")
		testName = regexp.MustCompile("`(Test[A-Za-z0-9_]+)")
	)
	rows := map[string]string{} // row id → the row
	for _, line := range strings.Split(matrix, "\n") {
		if m := rowID.FindStringSubmatch(line); m != nil {
			if _, dup := rows[m[1]]; dup {
				t.Errorf("the matrix has more than one row %s", m[1])
			}
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

	for _, id := range want {
		m := status.FindStringSubmatch(rows[id])
		switch {
		case m == nil:
			t.Errorf("row %s has no status (tested, partial, deferred)", id)
		case m[1] == "tested" && !evidence.MatchString(m[2]):
			t.Errorf("row %s is tested but names no test or CI job", id)
		case m[1] == "tested" && strings.Contains(m[2], "🔒"):
			t.Errorf("row %s is tested but defers part of it (🔒): make it partial", id)
		case m[1] != "tested" && !deferral.MatchString(m[2]):
			t.Errorf("row %s is %s but names no deferment (🔒 or D-NN)", id, m[1])
		}
	}

	// The module's code only: not checkouts or tools elsewhere in the tree.
	var tests strings.Builder
	for _, dir := range []string{"cmd", "internal", "test"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
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
	}
	named := testName.FindAllStringSubmatch(matrix, -1)
	if len(named) < 100 {
		t.Fatalf("test names not read from the matrix: %d", len(named))
	}
	for _, n := range named {
		if !strings.Contains(tests.String(), "\nfunc "+n[1]+"(") {
			t.Errorf("the matrix names %s, which is not a test", n[1])
		}
	}
}
