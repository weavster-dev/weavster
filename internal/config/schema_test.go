package config

import (
	"strings"
	"testing"
)

func TestValidateJSONRejectsMalformedJSON(t *testing.T) {
	if err := validateJSON([]byte(`{"flows":`)); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}

// TestParseValidChecksFlowSources: offline validation applies the flow
// source checks the schema cannot express.
func TestParseValidChecksFlowSources(t *testing.T) {
	for _, tt := range []struct {
		doc, want string
	}{
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: file, dir: /in}}\n", ""},
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: file, dir: in}}\n", "flows.adt: source.dir must be an absolute path"},
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: file, dir: /in, pattern: \"a/*\"}}\n", "flows.adt: source.pattern"},
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: http, address: \":9001\", path: /adt}}\n", ""},
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: http, address: \":0\"}}\n", "flows.adt: source.address needs a port"},
		{"version: \"1\"\nflows:\n  adt: {id: adt, source: {type: http, address: \":9001\", certFile: c.pem, keyFile: k.pem}}\n", "flows.adt: source.certFile and source.keyFile must be absolute paths"},
	} {
		_, err := ParseValid([]byte(tt.doc))
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%q: %v, want %q", tt.doc, err, tt.want)
		}
	}
}
