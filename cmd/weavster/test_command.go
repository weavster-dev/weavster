package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/weavster-dev/weavster/internal/codecs"
)

// fixture is a built-in transform fixture run by `weavster test`.
type fixture struct {
	name    string
	content []byte
	codec   string
}

// builtinFixtures returns the golden-path fixtures (no Postgres required,
// constraint #3). Full WASM transform execution is wired through the
// compiler+registry when modules are registered.
func builtinFixtures() []fixture {
	return []fixture{
		{name: "identity/hl7", codec: "hl7v2", content: []byte("MSH|^~\\&|A|B|C|D|20240101120000||ADT^A01|1|P|2.5\rPID|1||12345||DOE^JOHN\r")},
		{name: "identity/json", codec: "json", content: []byte(`{"patient":{"lastName":"Doe","firstName":"John"}}`)},
		{name: "identity/xml", codec: "xml", content: []byte(`<patient><lastName>Doe</lastName></patient>`)},
		{name: "identity/raw", codec: "raw", content: []byte("passthrough-bytes")},
	}
}

// runTransform applies the MVP transform (codec parse -> serialize round-trip).
func runTransform(codecName string, content []byte) error {
	c, err := codecs.Standard().Get(codecName)
	if err != nil {
		return err
	}
	v, err := c.Parse(content)
	if err != nil {
		return err
	}
	_, err = c.Serialize(v)
	return err
}

// testResult is one fixture outcome.
type testResult struct {
	Name    string `json:"name" xml:"name,attr"`
	Passed  bool   `json:"passed" xml:"-"`
	Failure string `json:"failure,omitempty" xml:"failure,omitempty"`
}

// runTest implements `weavster test [--filter NAME] [--format junit|json]
// [--output DIR]` (architecture §7).
func runTest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filter := fs.String("filter", "", "run fixtures whose name contains this substring")
	format := fs.String("format", "junit", "output format: junit|json")
	output := fs.String("output", "", "output directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var results []testResult
	for _, fx := range builtinFixtures() {
		if *filter != "" && !strings.Contains(fx.name, *filter) {
			continue
		}
		err := runTransform(fx.codec, fx.content)
		r := testResult{Name: fx.name, Passed: err == nil}
		if err != nil {
			r.Failure = err.Error()
		}
		results = append(results, r)
	}

	failures := 0
	for _, r := range results {
		if !r.Passed {
			failures++
		}
	}

	var err error
	switch *format {
	case "json":
		err = writeJSONResults(*output, stdout, results)
	default:
		err = writeJUnitResults(*output, stdout, results)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	if failures > 0 {
		return 1
	}
	return 0
}

func writeJSONResults(dir string, stdout io.Writer, results []testResult) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return writeOrPrint(dir, "results.json", data, stdout)
}

func writeJUnitResults(dir string, stdout io.Writer, results []testResult) error {
	failures := 0
	for _, r := range results {
		if !r.Passed {
			failures++
		}
	}
	suite := struct {
		XMLName  xml.Name     `xml:"testsuite"`
		Name     string       `xml:"name,attr"`
		Tests    int          `xml:"tests,attr"`
		Failures int          `xml:"failures,attr"`
		Cases    []testResult `xml:"testcase"`
	}{
		Name: "weavster", Tests: len(results), Failures: failures, Cases: results,
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	data = append([]byte(xml.Header), data...)
	return writeOrPrint(dir, "results.xml", data, stdout)
}

func writeOrPrint(dir, name string, data []byte, stdout io.Writer) error {
	if dir == "" {
		_, err := stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}
