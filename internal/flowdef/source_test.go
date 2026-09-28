package flowdef

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		{"recursive, moveTo outside", &Source{Type: "file", Dir: "/in", MoveTo: "/done", Recursive: true}, ""},
		{"recursive, moveTo inside", &Source{Type: "file", Dir: "/in", MoveTo: "/in/done", Recursive: true}, "must not be inside source.dir when recursive"},
		{"recursive, moveTo sibling prefix", &Source{Type: "file", Dir: "/in", MoveTo: "/inbox", Recursive: true}, ""},
		{"not recursive, moveTo inside", &Source{Type: "file", Dir: "/in", MoveTo: "/in/done"}, ""},
		{"http", &Source{Type: "http", Address: ":9001"}, ""},
		{"http with host", &Source{Type: "http", Address: "127.0.0.1:9001", Path: "/adt", Method: "PUT"}, ""},
		{"http without port", &Source{Type: "http", Address: "127.0.0.1"}, "source.address must be host:port"},
		{"http port zero", &Source{Type: "http", Address: ":0"}, "port from 1 to 65535"},
		{"http named port", &Source{Type: "http", Address: ":http"}, "port from 1 to 65535"},
		{"http port too big", &Source{Type: "http", Address: ":70000"}, "port from 1 to 65535"},
		{"mllp", &Source{Type: "mllp", Address: ":2575"}, ""},
		{"mllp without port", &Source{Type: "mllp", Address: "localhost"}, "source.address must be host:port"},
		{"mllp with TLS", &Source{Type: "mllp", Address: ":2575", CertFile: "/c", KeyFile: "/k"}, ""},
		{"mllp cert without key", &Source{Type: "mllp", Address: ":2575", CertFile: "/c"}, "go together"},
		{"mllp relative key", &Source{Type: "mllp", Address: ":2575", CertFile: "/c", KeyFile: "k"}, "must be absolute paths"},
		{"mllp with a path", &Source{Type: "mllp", Address: ":2575", Path: "/x"}, "takes only type, address, certFile, keyFile, frameStart, frameEnd, and ackMode"},
		{"mllp framing", &Source{Type: "mllp", Address: ":2575", FrameStart: "02", FrameEnd: "03", AckMode: "none"}, ""},
		{"mllp two-byte end", &Source{Type: "mllp", Address: ":2575", FrameEnd: "1c0a"}, ""},
		{"mllp start not hex", &Source{Type: "mllp", Address: ":2575", FrameStart: "VT"}, "source.frameStart must be one byte in hex"},
		{"mllp start two bytes", &Source{Type: "mllp", Address: ":2575", FrameStart: "0B0B"}, "frameStart must be one byte"},
		{"mllp end three bytes", &Source{Type: "mllp", Address: ":2575", FrameEnd: "1C0D0A"}, "frameEnd must be one or two bytes"},
		{"mllp printable start", &Source{Type: "mllp", Address: ":2575", FrameStart: "41"}, "frameStart 41 can occur in a message"},
		{"mllp CR end", &Source{Type: "mllp", Address: ":2575", FrameEnd: "0D"}, "frameEnd starts with 0D"},
		{"mllp same bytes", &Source{Type: "mllp", Address: ":2575", FrameStart: "1C"}, "must differ"},
		{"mllp repeated end byte", &Source{Type: "mllp", Address: ":2575", FrameEnd: "1C1C"}, "the two bytes of frameEnd must differ"},
		{"mllp DEL start", &Source{Type: "mllp", Address: ":2575", FrameStart: "7F"}, ""},
		{"mllp ackMode", &Source{Type: "mllp", Address: ":2575", AckMode: "enhanced"}, "ackMode must be original or none"},
		{"http framing", &Source{Type: "http", Address: ":9001", AckMode: "none"}, "apply only to mllp sources"},
		{"database", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id"}, ""},
		{"database with update", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Update: &SourceUpdate{Table: "his.t", Key: "id", Set: map[string]string{"done": "1"}}, MaxRows: 5, TimeoutMs: 2000, PollIntervalMs: 1000}, ""},
		{"database with a dir", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Dir: "/in"}, "a database source takes only"},
		{"database driver", &Source{Type: "database", Driver: "mysql", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT 1", IDColumn: "id"}, "source.driver must be"},
		{"database dsnEnv", &Source{Type: "database", Driver: "sqlite", DSNEnv: "HOME", Query: "SELECT 1", IDColumn: "id"}, "source.dsnEnv must name"},
		{"database query", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "DELETE FROM t", IDColumn: "id"}, "source.query must be one SELECT"},
		{"database idColumn", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT 1"}, "source.idColumn is required"},
		{"database update table", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Update: &SourceUpdate{Table: "a.b.c", Key: "id", Set: map[string]string{"d": "1"}}}, "source.update.table must be"},
		{"database update key", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Update: &SourceUpdate{Table: "t", Key: "i d", Set: map[string]string{"d": "1"}}}, "source.update.key must be"},
		{"database update set", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Update: &SourceUpdate{Table: "t", Key: "id"}}, "source.update.set must give"},
		{"database update column", &Source{Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Query: "SELECT id FROM t", IDColumn: "id", Update: &SourceUpdate{Table: "t", Key: "id", Set: map[string]string{"a-b": "1"}}}, "source.update.set: column"},
		{"file with a query", &Source{Type: "file", Dir: "/in", Query: "SELECT 1"}, "apply only to database sources"},
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
		{`{"id":"a","source":{"type":"file","dir":"/in","recursive":true}}`, true},
		{`{"id":"a","source":{"type":"file","dir":"/in","subdirs":true}}`, false},
		{`{"id":"a","source":null}`, true},
		{`{"id":"a","source":{"type":"mllp","address":":2575"}}`, true},
		{`{"id":"a","source":{"type":"mllp"}}`, false},
		{`{"id":"a","source":{"type":"mllp","address":":2575","path":"/x"}}`, false},
		{`{"id":"a","source":{"type":"mllp","address":":2575","certFile":"/c","keyFile":"/k"}}`, true},
		{`{"id":"a","source":{"type":"mllp","address":":2575","certFile":"/c"}}`, false},
		{`{"id":"a","destinations":[{"name":"l","type":"mllp","address":"l:1","tls":true,"caFile":"/ca.pem"}]}`, true},
		{`{"id":"a","source":{"type":"mllp","address":":2575","frameStart":"02","frameEnd":"1C0D","ackMode":"none"},"destinations":[{"name":"l","type":"mllp","address":"l:1","frameStart":"02","frameEnd":"03","ackMode":"original"}]}`, true},
		{`{"id":"a","source":{"type":"mllp","address":":2575","frameEnd":"1C0D0A"}}`, false},
		{`{"id":"a","source":{"type":"mllp","address":":2575","ackMode":"commit"}}`, false},
		{`{"id":"a","source":{"type":"http","address":":9001","ackMode":"none"}}`, false},
		{`{"id":"a","destinations":[{"name":"l","type":"http","url":"https://x","frameStart":"02"}]}`, false},
		{`{"id":"a","destinations":[{"name":"l","type":"mllp","address":"l:1","caFile":"/ca.pem"}]}`, false},
		{`{"id":"a","destinations":[{"name":"l","type":"mllp","address":"l:1","tls":false,"caFile":"/ca.pem"}]}`, false},
		{`{"id":"a","source":{"type":"mllp","address":":2575","username":"lab","passwordEnv":"WEAVSTER_SOURCE_LAB"}}`, false},
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
		{`{"name":"a","type":"mllp","address":"lab:2575","timeoutMs":5000}`, true},
		{`{"name":"a","type":"mllp","address":"lab:2575","method":"PUT"}`, false},
		{`{"name":"a","type":"mllp","address":"lab:2575","url":"https://x"}`, false},
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
		{Flow{InputFormat: "json", Source: &Source{Type: "database"}}, ""},
		{Flow{InputFormat: "xml", Source: &Source{Type: "database"}}, "a database source sends JSON messages (one object per row): inputFormat must be json"},
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

func TestCheckTransforms(t *testing.T) {
	set := json.RawMessage(`{"steps":[{"destinationSet":{"exclude":["b"],"when":"x"}}]}`)
	plain := json.RawMessage(`{"steps":[{"set":{"field":"a","expr":"b"}}]}`)
	dest := func(name string) Destination { return Destination{Name: name, Type: "file", Dir: "/d"} }
	for _, tt := range []struct {
		name string
		f    Flow
		want string
	}{
		{"none", Flow{}, ""},
		{"ok", Flow{Transform: set, Destinations: []Destination{dest("a"), dest("b")}}, ""},
		{"not a transform", Flow{Transform: json.RawMessage(`"x"`)}, ""},
		{"unknown name", Flow{Transform: set, Destinations: []Destination{dest("a")}}, `destinationSet excludes "b", which is not a destination of the flow`},
		{"in a destination transform", Flow{Destinations: []Destination{dest("a"), {Name: "b", Type: "file", Dir: "/d", Transform: set}}}, "destination b: transform: destinationSet can only be used in the flow's transform"},
		{"in a response transform", Flow{Destinations: []Destination{{Name: "b", Type: "http", URL: "https://x", ResponseTransform: set}}}, "destination b: responseTransform: destinationSet can only be used"},
		{"plain destination transform", Flow{Destinations: []Destination{{Name: "b", Type: "file", Dir: "/d", Transform: plain}}}, ""},
		{"build in a response transform", Flow{Destinations: []Destination{{Name: "b", Type: "http", URL: "https://x", ResponseTransform: json.RawMessage(`{"steps":[{"build":{"template":"x"}}]}`)}}}, "responseTransform: build cannot be used here"},
		{"build not last in a response transform", Flow{Destinations: []Destination{{Name: "b", Type: "http", URL: "https://x", ResponseTransform: json.RawMessage(`{"steps":[{"build":{"template":"x"}},{"set":{"field":"a","expr":"b"}}]}`)}}}, "responseTransform: build cannot be used here"},
		{"build in a destination transform", Flow{Destinations: []Destination{{Name: "b", Type: "file", Dir: "/d", Transform: json.RawMessage(`{"steps":[{"build":{"template":"x","format":"text"}}]}`)}}}, ""},
	} {
		err := CheckTransforms(tt.f)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestCheckDestinations(t *testing.T) {
	for _, tt := range []struct {
		dest Destination
		want string
	}{
		{Destination{Name: "a", Type: "file", Dir: "/out"}, ""},
		{Destination{Name: "a", Type: "file"}, ""}, // reported as required when used
		{Destination{Name: "a", Type: "file", Dir: "out"}, `destination a: dir must be an absolute path, got "out"`},
		{Destination{Name: "a", Type: "file", Dir: "../etc"}, "must be an absolute path"},
		{Destination{Name: "a", Type: "http", URL: "https://x"}, ""},
		{Destination{Name: "a", Type: "mllp", TLS: true}, ""},
		{Destination{Name: "a", Type: "mllp", TLS: true, CAFile: "/tls/ca.pem"}, ""},
		{Destination{Name: "a", Type: "mllp", CAFile: "/tls/ca.pem"}, "caFile needs tls: true"},
		{Destination{Name: "a", Type: "mllp", TLS: true, CAFile: "ca.pem"}, "caFile must be an absolute path"},
		{Destination{Name: "a", Type: "http", URL: "https://x", TLS: true}, "apply only to mllp destinations"},
		{Destination{Name: "a", Type: "mllp", FrameStart: "02", FrameEnd: "03", AckMode: "none"}, ""},
		{Destination{Name: "a", Type: "mllp", FrameEnd: "0A"}, "destination a: frameEnd starts with 0A"},
		{Destination{Name: "a", Type: "file", Dir: "/out", AckMode: "none"}, "apply only to mllp destinations"},
		{Destination{Name: "a", Type: "database", Driver: "postgres", DSNEnv: "WEAVSTER_DB_LAB", Table: "lab.results", Columns: map[string]string{"mrn": "patient.mrn"}, KeyColumn: "k"}, ""},
		{Destination{Name: "a", Type: "database", Driver: "mysql", DSNEnv: "WEAVSTER_DB_LAB", Table: "t", Columns: map[string]string{"a": "a"}}, "driver must be postgres or sqlite"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "PATH", Table: "t", Columns: map[string]string{"a": "a"}}, "dsnEnv must name"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Table: "a.b.c", Columns: map[string]string{"a": "a"}}, "table must be"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Table: "t"}, "columns must map"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Table: "t", Columns: map[string]string{"a b": "a"}}, "must be a name"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Table: "t", Columns: map[string]string{"a": "a"}, KeyColumn: "k-1"}, "keyColumn must be"},
		{Destination{Name: "a", Type: "database", Driver: "sqlite", DSNEnv: "WEAVSTER_DB_X", Table: "t", Columns: map[string]string{"a": ".a"}}, "path must be"},
		{Destination{Name: "a", Type: "http", URL: "https://x", Table: "t"}, "apply only to database destinations"},
	} {
		err := CheckDestinations(Flow{Destinations: []Destination{tt.dest}})
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%+v: %v, want %q", tt.dest, err, tt.want)
		}
	}
}

// TestWithinResolvesLinks: containment holds through a symbolic link, also
// for a directory that does not exist yet.
func TestWithinResolvesLinks(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		path, dir string
		want      bool
	}{
		{filepath.Join(real, "done"), link, true},
		{filepath.Join(link, "done", "later"), real, true},
		{real, link, true},
		{filepath.Join(t.TempDir(), "x"), link, false},
		{"/in/done", "/in", true},
		{"/inbox", "/in", false},
	} {
		if got := Within(tt.path, tt.dir); got != tt.want {
			t.Errorf("Within(%s, %s) = %v, want %v", tt.path, tt.dir, got, tt.want)
		}
	}
}

func TestDependencies(t *testing.T) {
	f := Flow{DependsOn: []string{"a", "b"}, Destinations: []Destination{
		{Name: "x", Type: "flow", Flow: "c"}, {Name: "y", Type: "flow", Flow: "a"}, {Name: "z", Type: "file", Dir: "/o"},
	}}
	if got := strings.Join(f.Dependencies(), ","); got != "a,b,c" {
		t.Errorf("Dependencies = %s", got)
	}
	for _, tt := range []struct {
		dest string
		ok   bool
	}{
		{`{"name":"a","type":"flow","flow":"next"}`, true},
		{`{"name":"a","type":"flow"}`, false},
		{`{"name":"a","type":"flow","flow":"next","url":"https://x"}`, false},
		{`{"name":"a","type":"http","url":"https://x","flow":"next"}`, false},
		{`{"name":"a","type":"flow","flow":"bad id"}`, false},
	} {
		if err := ValidateJSON([]byte(`{"id":"f","destinations":[` + tt.dest + `]}`)); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.dest, err)
		}
	}
}
