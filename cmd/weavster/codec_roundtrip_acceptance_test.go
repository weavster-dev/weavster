package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestCodecRoundTrips: `weavster test` runs a round trip through every
// data codec — HL7 v2 (subcomponents, escapes, custom delimiters), JSON
// (exact numbers), XML (namespaces, mixed content, comments, processing
// instructions), delimited (RFC 4180 quoting), and raw binary — and each
// comes back byte for byte.
func TestCodecRoundTrips(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"test", "--format", "json"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s %s", code, out.String(), errb.String())
	}
	var results []testResult
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	passed := map[string]bool{}
	for _, r := range results {
		passed[r.Name] = r.Passed
	}
	for _, name := range []string{"identity/hl7", "identity/json", "identity/xml", "identity/delimited", "identity/raw"} {
		if !passed[name] {
			t.Errorf("%s did not pass: %s", name, out.String())
		}
	}
}
