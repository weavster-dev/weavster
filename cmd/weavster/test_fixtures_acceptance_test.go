package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo is a user's repository: a config-as-code document with an HL7 v2
// flow (filter, maps, destinationSet, and a destination with its own
// transform), fixture files for it in a sub-directory, and an input file.
func testRepo(t *testing.T, failing bool) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"weavster.yaml": `version: "1"
flows:
  adt:
    id: adt
    name: ADT
    inputFormat: hl7v2
    transform:
      name: adt-to-json
      steps:
        - filter: {when: "MSH.9.2 == 'A01'", action: accept}
        - map: {from: PID.5.1, to: patient.lastName}
        - map: {from: PID.3.1, to: patient.mrn}
        - destinationSet: {exclude: [archive], when: "patient.lastName == 'TEST'"}
    destinations:
      - {name: archive, type: file, dir: /var/lib/weavster/archive}
      - name: his
        type: file
        dir: /var/lib/weavster/his
        transform:
          name: to-his
          steps:
            - map: {from: patient.mrn, to: id}
`,
		"docker-compose.yml": "services: {}\n", // not a config document: left alone
		"tests/a01.hl7":      "MSH|^~\\&|LAB|H|EHR|H|20240101120000||ADT^A01|1|P|2.5\rPID|1||12345^^^MRN||DOE^JOHN\r",
		"tests/adt.test.yaml": `flow: adt
cases:
  - name: admit
    inputFile: a01.hl7
    expect:
      output: {patient: {lastName: DOE, mrn: "12345"}}
      excluded: []
      destinations:
        his: {output: {id: "12345"}}
  - name: update is filtered
    input: "MSH|^~\\&|LAB|H|EHR|H|20240101120000||ADT^A08|2|P|2.5\rPID|1||12345^^^MRN||DOE^JOHN\r"
    expect: {status: filtered}
  - name: test patient skips the archive
    input: "MSH|^~\\&|LAB|H|EHR|H|20240101120000||ADT^A01|3|P|2.5\rPID|1||999^^^MRN||TEST^T\r"
    expect: {excluded: [archive]}
  - name: not hl7
    input: "hello"
    expect: {status: errored, error: "HL7 v2"}
`,
		".git/ignored.test.yaml": "not: a fixture\n", // hidden directories are skipped
	}
	if failing {
		files["tests/wrong.test.json"] = `{"flow":"adt","cases":[{"name":"wrong name","input":"MSH|^~\\&|LAB|H|EHR|H|20240101120000||ADT^A01|1|P|2.5\rPID|1||12345^^^MRN||DOE^JOHN\r","expect":{"output":{"patient":{"lastName":"SMITH"}}}}]}`
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestFixtures: `weavster test PATH` finds the fixture files and the flows
// of the config-as-code documents under PATH and runs every case offline
// through the real transforms: input formats, filters, maps,
// destinationSet, and destination transforms; a failing case says why and
// exits 1; results come as JSON or JUnit, printed or written to a
// directory.
func TestFixtures(t *testing.T) {
	repo := testRepo(t, false)
	var out, errb bytes.Buffer
	if code := run([]string{"test", "--format", "json", repo}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	var results []testResult
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	passed := map[string]bool{}
	for _, r := range results {
		passed[strings.TrimPrefix(r.Name, filepath.ToSlash(repo)+"/")] = r.Passed
	}
	for _, name := range []string{"identity/hl7", "tests/adt/admit", "tests/adt/update is filtered", "tests/adt/test patient skips the archive", "tests/adt/not hl7"} {
		if !passed[name] {
			t.Errorf("%s did not pass: %s", name, out.String())
		}
	}
	if len(results) != 9 { // 5 built-in round trips and 4 cases
		t.Errorf("%d results: %s", len(results), out.String())
	}

	// A failing case: exit 1, the reason on stderr and in JUnit.
	repo = testRepo(t, true)
	out.Reset()
	errb.Reset()
	results_dir := t.TempDir()
	if code := run([]string{"test", "--output", results_dir, repo}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, errb.String())
	}
	junit, err := os.ReadFile(filepath.Join(results_dir, "results.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`failures="1"`, `name="` + filepath.ToSlash(repo) + `/tests/wrong/wrong name"`, "output differs at patient.lastName"} {
		if !strings.Contains(string(junit), want) || !strings.Contains(errb.String()+string(junit), "FAIL") && want == "FAIL" {
			t.Errorf("JUnit lacks %q: %s", want, junit)
		}
	}
	if !strings.Contains(errb.String(), "FAIL "+filepath.ToSlash(repo)+"/tests/wrong/wrong name: output differs at patient.lastName") {
		t.Errorf("stderr = %s", errb.String())
	}

	// --filter narrows the cases; one that matches nothing fails.
	out.Reset()
	if code := run([]string{"test", "--format", "json", "--filter", "filtered", repo}, strings.NewReader(""), &out, &errb); code != 0 || strings.Count(out.String(), `"name"`) != 1 {
		t.Errorf("--filter: exit %d %s", code, out.String())
	}
	errb.Reset()
	if code := run([]string{"test", "--filter", "no such case", repo}, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), `no case matches --filter "no such case"`) {
		t.Errorf("empty --filter: exit %d %s", code, errb.String())
	}
}

// TestFixtureErrors: usage errors exit 2; broken fixtures fail with the
// reason.
func TestFixtureErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"bad format", []string{"test", "--format", "tap"}, 2, `--format must be junit or json, got "tap"`},
		{"missing path", []string{"test", "/nonexistent/weavster"}, 2, "no such file or directory"},
		{"unknown flag", []string{"test", "--frobnicate"}, 2, "frobnicate"},
	} {
		var out, errb bytes.Buffer
		if code := run(tt.args, strings.NewReader(""), &out, &errb); code != tt.code || !strings.Contains(errb.String(), tt.want) {
			t.Errorf("%s: exit %d %q", tt.name, code, errb.String())
		}
	}

	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.yaml":            "version: \"1\"\nflows:\n  dup: {id: dup}\n",
		"b.yaml":            "version: \"1\"\nflows:\n  dup: {id: dup}\n  ok: {id: ok, transform: {steps: [{map: {from: a, to: b}}]}}\n",
		"bad.yaml":          "version: \"1\"\nflowz: {}\n",
		"dup.test.yaml":     "flow: dup\ncases: [{name: x, input: '{}'}]\n",
		"unknown.test.yaml": "flow: nope\ncases: [{name: x, input: '{}'}]\n",
		"typo.test.yaml":    "flow: ok\ncasez: []\n",
		"noflow.test.yaml":  "cases: [{name: x, input: '{}'}]\n",
		"nocases.test.yaml": "flow: ok\n",
		"cases.test.yaml":   "flow: ok\ncases:\n  - {name: both, input: '{}', inputFile: x.json}\n  - {name: nofile, inputFile: missing.json}\n  - {input: '{}'}\n  - {name: dest, input: '{}', expect: {destinations: {nope: {status: transformed}}}}\n  - {name: text, input: '{\"a\":1}', expect: {outputText: 'x'}}\n  - {name: status, input: 'not json', expect: {status: transformed}}\n  - {name: err, input: 'not json', expect: {status: errored, error: 'no such text'}}\n  - {name: notjson, input: 'x', expect: {status: errored}}\n  - {name: excl, input: '{}', expect: {excluded: [a]}}\n  - {name: shape, input: '{\"a\":1}', expect: {output: {a: {b: 1}}}}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"test", "--format", "json", dir}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{
		"bad.yaml: ", "defined in more than one document", "no config-as-code document under the given paths defines flow nope",
		"field casez not found", "flow is required", "no cases", "give input or inputFile (one of them)", "inputFile: open",
		"every case needs a name of its own", "destination nope: no transform result", `output is "{\"a\":1,\"b\":1}", want "x"`,
		"status is errored (", "want it to contain \"no such text\"", "excluded destinations are [], want [a]", "output differs at a (not an object)",
	} {
		if !strings.Contains(errb.String(), want) {
			t.Errorf("stderr lacks %q:\n%s", want, errb.String())
		}
	}
	if strings.Contains(errb.String(), "notjson") {
		t.Errorf("a passing case failed: %s", errb.String())
	}
}
