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
		{"mllp", &Source{Type: "mllp", Address: ":2575"}, ""},
		{"mllp without port", &Source{Type: "mllp", Address: "localhost"}, "source.address must be host:port"},
		{"mllp with TLS fields", &Source{Type: "mllp", Address: ":2575", CertFile: "/c", KeyFile: "/k"}, "takes only type and address"},
		{"http secured", &Source{Type: "http", Address: ":9001", Username: "lab", PasswordEnv: "WEAVSTER_SOURCE_LAB", CertFile: "/tls/c.pem", KeyFile: "/tls/k.pem"}, ""},
		{"http user without password", &Source{Type: "http", Address: ":9001", Username: "lab"}, "go together"},
		{"http password without user", &Source{Type: "http", Address: ":9001", PasswordEnv: "LAB_PW"}, "go together"},
		{"http cert without key", &Source{Type: "http", Address: ":9001", CertFile: "/tls/c.pem"}, "go together"},
		{"http relative cert", &Source{Type: "http", Address: ":9001", CertFile: "c.pem", KeyFile: "/tls/k.pem"}, "must be absolute paths"},
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
		{`{"id":"a","source":{"type":"mllp","address":":2575"}}`, true},
		{`{"id":"a","source":{"type":"mllp"}}`, false},
		{`{"id":"a","source":{"type":"mllp","address":":2575","path":"/x"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","path":"/adt","method":"PUT"}}`, true},
		{`{"id":"a","source":{"type":"http"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","path":"adt"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","method":"GET"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","dir":"/in"}}`, false},
		{`{"id":"a","source":{"type":"file","dir":"/in","address":":9001"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","username":"lab","passwordEnv":"WEAVSTER_SOURCE_LAB","certFile":"/c","keyFile":"/k"}}`, true},
		{`{"id":"a","source":{"type":"http","address":":9001","username":"lab"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","keyFile":"/k"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","username":"a:b","passwordEnv":"WEAVSTER_SOURCE_LAB"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","username":"lab","passwordEnv":"WEAVSTER_SOURCE_LAB-PW"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","username":"lab","passwordEnv":"AWS_SECRET_ACCESS_KEY"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","password":"secret"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","certFile":"","keyFile":""}}`, false},
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

// TestDestinationRequestOptionsSchema: method, timeoutMs, and maxRedirects
// are http-only and bounded.
func TestDestinationRequestOptionsSchema(t *testing.T) {
	for _, tt := range []struct {
		dest string
		ok   bool
	}{
		{`{"name":"a","type":"http","url":"https://x","method":"PATCH","timeoutMs":1000,"maxRedirects":10}`, true},
		{`{"name":"a","type":"http","url":"https://x","method":"GET"}`, false},
		{`{"name":"a","type":"http","url":"https://x","timeoutMs":999}`, false},
		{`{"name":"a","type":"http","url":"https://x","maxRedirects":11}`, false},
		{`{"name":"a","type":"file","dir":"/out","method":"PUT"}`, false},
		{`{"name":"a","type":"file","dir":"/out","maxRedirects":1}`, false},
		{`{"name":"a","type":"file","dir":"/out","method":null,"timeoutMs":null}`, true},
		{`{"name":"a","type":"http","url":"https://x","timeoutMs":120001}`, false},
	} {
		if err := ValidateJSON([]byte(`{"id":"f","destinations":[` + tt.dest + `]}`)); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.dest, err)
		}
	}
	if err := ValidateJSON([]byte(`{"id":"f","source":{"type":"http","address":":9001","readTimeoutMs":500}}`)); err == nil {
		t.Error("readTimeoutMs under 1000 accepted")
	}
}

func TestCheckInput(t *testing.T) {
	yes := true
	for _, tt := range []struct {
		f    Flow
		want string
	}{
		{Flow{}, ""},
		{Flow{InputFormat: "delimited", Delimited: &Delimited{Delimiter: ";", Header: &yes}}, ""},
		{Flow{InputFormat: "json", Delimited: &Delimited{}}, "delimited applies only to inputFormat delimited"},
		{Flow{Delimited: &Delimited{}}, "delimited applies only to inputFormat delimited"},
	} {
		err := CheckInput(tt.f)
		if (tt.want == "") != (err == nil) || (err != nil && err.Error() != tt.want) {
			t.Errorf("%+v: %v, want %q", tt.f, err, tt.want)
		}
	}
	for _, tt := range []struct {
		doc string
		ok  bool
	}{
		{`{"id":"a","inputFormat":"delimited","delimited":{"delimiter":"\t","header":false}}`, true},
		{`{"id":"a","inputFormat":"delimited","delimited":{"delimiter":"::"}}`, false},
		{`{"id":"a","inputFormat":"delimited","delimited":{"quote":"'"}}`, false},
	} {
		if err := ValidateJSON([]byte(tt.doc)); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.doc, err)
		}
	}
}
