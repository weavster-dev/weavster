package main

import (
	"bytes"
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

// fixture is a built-in codec round-trip fixture run by `weavster test`:
// content parsed and serialized by codec must come back unchanged.
type fixture struct {
	name    string
	content []byte
	codec   string
}

// builtinFixtures returns the codec round-trip fixtures (#107 D-74), each
// in its codec's canonical form, so the output must equal the input.
func builtinFixtures() []fixture {
	return []fixture{
		{name: "identity/hl7", codec: "hl7v2", content: []byte("MSH|^~\\&|A|B|C|D|20240101120000||ADT^A01|1|P|2.5\r" +
			"PID|1||12345^^^HOSP&1.2.3&ISO~678^^^SSN||O\\T\\BRIEN^ANN\r" +
			"MSH#$%!@#A#B#C#D#20240101120000##ADT$A08#2!F!3#P#2.4\rPID#1##1$$$H@O\r")},
		{name: "identity/json", codec: "json", content: []byte(`{"patient":{"firstName":"John","ids":[12345678901234567890123,1.50],"lastName":"Doe","note":"<b> & ü"}}`)},
		{name: "identity/xml", codec: "xml", content: []byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
			`<!-- ADT --><p:patient xmlns:p="urn:hl7-org:v3" xmlns="urn:local" id="1"><p:name use="L">Doe &amp; Sons</p:name>` +
			`Admitted <b>today</b> via ER<?audit ok?><flag/></p:patient>`)},
		{name: "identity/delimited", codec: "delimited", content: []byte("id|name|note\n1|\"Doe|John\"|\"say \"\"hi\"\"\"\n2|Ann|\"two\nlines\"")},
		{name: "identity/raw", codec: "raw", content: []byte("passthrough\x00\x01\xff\r\n")},
	}
}

// runTransform parses content with the codec, serializes it again, and
// fails unless the result is content.
func runTransform(codecName string, content []byte) error {
	c, err := codecs.Standard().Get(codecName)
	if err != nil {
		return err
	}
	v, err := c.Parse(content)
	if err != nil {
		return err
	}
	out, err := c.Serialize(v)
	if err != nil {
		return err
	}
	if !bytes.Equal(out, content) {
		return fmt.Errorf("%s: the round trip changed the content (%d bytes in, %d out)", codecName, len(content), len(out))
	}
	return nil
}

// testResult is one fixture outcome.
type testResult struct {
	Name    string `json:"name" xml:"name,attr"`
	Passed  bool   `json:"passed" xml:"-"`
	Failure string `json:"failure,omitempty" xml:"failure,omitempty"`
}

// runTest implements `weavster test [--filter NAME] [--format junit|json]
// [--output DIR] [PATH...]` (architecture §7, #107 D-101): the built-in
// codec round trips, and the fixture files under the paths (default .),
// run offline through the flows of the config-as-code documents there.
// It exits 0 when every case passed, 1 when one failed (or nothing
// matched --filter), and 2 on a usage error or when the results cannot be
// written.
func runTest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filter := fs.String("filter", "", "run the cases whose name contains this text")
	format := fs.String("format", "junit", "output format: junit|json")
	output := fs.String("output", "", "write results.xml or results.json into this directory")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Usage: weavster test [--filter NAME] [--format junit|json] [--output DIR] [PATH...]")
		fs.PrintDefaults()
	}
	if code, ok := parseFlags(fs, args, stderr); !ok {
		if code == 0 {
			fs.SetOutput(stdout)
			fs.Usage()
		}
		return code
	}
	if *format != "junit" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "Error: --format must be junit or json, got %q\n", *format)
		return 2
	}
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	found, err := discover(paths)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	var results []testResult // only what --filter selects is run
	for _, fx := range builtinFixtures() {
		if !strings.Contains(fx.name, *filter) {
			continue
		}
		err := runTransform(fx.codec, fx.content)
		r := testResult{Name: fx.name, Passed: err == nil}
		if err != nil {
			r.Failure = err.Error()
		}
		results = append(results, r)
	}
	results = append(results, found.runFixtures(*filter)...)

	failures := 0
	for _, r := range results {
		if !r.Passed {
			failures++
			_, _ = fmt.Fprintf(stderr, "FAIL %s: %s\n", r.Name, r.Failure)
		}
	}
	if *format == "json" {
		err = writeJSONResults(*output, stdout, results)
	} else {
		err = writeJUnitResults(*output, stdout, results)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	switch {
	case len(results) == 0:
		_, _ = fmt.Fprintf(stderr, "Error: no case matches --filter %q\n", *filter)
		return 1
	case failures > 0:
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
