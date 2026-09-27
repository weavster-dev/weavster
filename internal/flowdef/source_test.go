package flowdef

import (
	"strings"
	"testing"
)

func TestCheckSource(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  *Source
		want string
	}{
		{"none", nil, ""},
		{"minimal", &Source{Type: "file", Dir: "/in"}, ""},
		{"full", &Source{Type: "file", Dir: "/in", Pattern: "*.hl7", PollIntervalMs: 500, MoveTo: "/in/done"}, ""},
		{"relative dir", &Source{Type: "file", Dir: "in"}, "source.dir must be an absolute path"},
		{"relative moveTo", &Source{Type: "file", Dir: "/in", MoveTo: "done"}, "source.moveTo must be an absolute path"},
		{"moveTo is dir", &Source{Type: "file", Dir: "/in", MoveTo: "/in/"}, "must differ from source.dir"},
		{"pattern with a path", &Source{Type: "file", Dir: "/in", Pattern: "../*"}, "without path separators"},
		{"bad pattern", &Source{Type: "file", Dir: "/in", Pattern: "[a"}, "syntax error in pattern"},
	} {
		err := CheckSource(tt.src)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
	// The schema accepts only file sources with the documented keys.
	for _, tt := range []struct {
		doc string
		ok  bool
	}{
		{`{"id":"a","source":{"type":"file","dir":"/in","pattern":"*.json","pollIntervalMs":100,"moveTo":"/done"}}`, true},
		{`{"id":"a","source":{"type":"ftp","dir":"/in"}}`, false},
		{`{"id":"a","source":{"type":"file"}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","pollIntervalMs":50}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","recursive":true}}`, false},
	} {
		if err := ValidateJSON([]byte(tt.doc)); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.doc, err)
		}
	}
}
