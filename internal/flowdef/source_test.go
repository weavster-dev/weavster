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
		{"dir is moveTo/rejected", &Source{Type: "file", Dir: "/data/rejected", MoveTo: "/data"}, "must not be moveTo/rejected"},
		{"pattern with a path", &Source{Type: "file", Dir: "/in", Pattern: "../*"}, "without path separators"},
		{"bad pattern", &Source{Type: "file", Dir: "/in", Pattern: "[a"}, "syntax error in pattern"},
		{"http", &Source{Type: "http", Address: ":9001"}, ""},
		{"http with host", &Source{Type: "http", Address: "127.0.0.1:9001", Path: "/adt", Method: "PUT"}, ""},
		{"http without port", &Source{Type: "http", Address: "127.0.0.1"}, "source.address must be host:port"},
		{"http port zero", &Source{Type: "http", Address: ":0"}, "port from 1 to 65535"},
		{"http named port", &Source{Type: "http", Address: ":http"}, "port from 1 to 65535"},
		{"http port too big", &Source{Type: "http", Address: ":70000"}, "port from 1 to 65535"},
	} {
		err := CheckSource(tt.src)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
	// The schema accepts only file and http sources with their own keys.
	for _, tt := range []struct {
		doc string
		ok  bool
	}{
		{`{"id":"a","source":{"type":"file","dir":"/in","pattern":"*.json","pollIntervalMs":100,"moveTo":"/done"}}`, true},
		{`{"id":"a","source":{"type":"ftp","dir":"/in"}}`, false},
		{`{"id":"a","source":{"type":"file"}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","pollIntervalMs":50}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","recursive":true}}`, false},
		{`{"id":"a","source":null}`, true},
		{`{"id":"a","source":{"type":"http","address":":9001","path":"/adt","method":"PUT"}}`, true},
		{`{"id":"a","source":{"type":"http"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","path":"adt"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","method":"GET"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","dir":"/in"}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","address":":9001"}}`, false},
	} {
		if err := ValidateJSON([]byte(tt.doc)); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.doc, err)
		}
	}
}

func TestSourceKind(t *testing.T) {
	for _, tt := range []struct {
		f    Flow
		want string
	}{
		{Flow{}, ""},
		{Flow{SourceType: "hl7"}, "hl7"},
		{Flow{SourceType: "hl7", Source: &Source{Type: "http"}}, "http"},
	} {
		if got := tt.f.SourceKind(); got != tt.want {
			t.Errorf("%+v: %q, want %q", tt.f, got, tt.want)
		}
	}
}
