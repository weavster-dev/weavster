package webui

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// TestHandler: the UI's files are served with their types, the CSP, and
// no caching; only GET and HEAD are allowed.
func TestHandler(t *testing.T) {
	h := Handler()
	for _, tt := range []struct {
		method, path string
		status       int
		ctype, body  string
	}{
		{http.MethodGet, "/", http.StatusOK, "text/html", "<title>Weavster topology</title>"},
		{http.MethodGet, "/app.js", http.StatusOK, "javascript", "WeavsterUI"},
		{http.MethodGet, "/app.css", http.StatusOK, "text/css", ".node"},
		{http.MethodHead, "/app.js", http.StatusOK, "javascript", ""},
		{http.MethodGet, "/missing.js", http.StatusNotFound, "", ""},
		{http.MethodPost, "/", http.StatusMethodNotAllowed, "", ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
		if rec.Code != tt.status || !strings.Contains(rec.Header().Get("Content-Type"), tt.ctype) || !strings.Contains(rec.Body.String(), tt.body) {
			t.Errorf("%s %s = %d %q %.60q", tt.method, tt.path, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		if tt.status == http.StatusOK && (rec.Header().Get("Content-Security-Policy") != CSP || rec.Header().Get("Cache-Control") != "no-cache") {
			t.Errorf("%s %s headers = %v", tt.method, tt.path, rec.Header())
		}
	}
}

func file(t *testing.T, name string) string {
	t.Helper()
	b, err := files.ReadFile("static/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPageHasNoInlineCode: the page's scripts and styles come from its own
// files (the CSP forbids inline ones), and every file it names exists.
func TestPageHasNoInlineCode(t *testing.T) {
	page := file(t, "index.html")
	for _, m := range regexp.MustCompile(`<script([^>]*)>([^<]*)</script>`).FindAllStringSubmatch(page, -1) {
		if !strings.Contains(m[1], "src=") || strings.TrimSpace(m[2]) != "" {
			t.Errorf("inline script in index.html: %s", m[0])
		}
	}
	if strings.Count(page, "<script") != len(regexp.MustCompile(`<script[^>]*>[^<]*</script>`).FindAllString(page, -1)) {
		t.Error("a script tag in index.html is not an empty <script src=...></script>")
	}
	if regexp.MustCompile(`(?i)\son\w+\s*=`).MatchString(page) || strings.Contains(page, "<style") || strings.Contains(page, "style=") {
		t.Error("inline event handler or style in index.html")
	}
	for _, m := range regexp.MustCompile(`(?:src|href)="([^"#]+)"`).FindAllStringSubmatch(page, -1) {
		if _, err := files.ReadFile("static/" + m[1]); err != nil {
			t.Errorf("index.html names %s: %v", m[1], err)
		}
	}
}

// TestReadOnly: the script sends only sign-in and sign-out POSTs, and
// otherwise only reads the topology; it has no controls that change
// anything.
func TestReadOnly(t *testing.T) {
	js := file(t, "app.js")
	calls := regexp.MustCompile(`request\('([A-Z]+)',\s*([^,)]+)`).FindAllStringSubmatch(js, -1)
	if len(calls) != 3 {
		t.Fatalf("request calls = %v", calls)
	}
	allowed := map[string]bool{"POST 'auth/login'": true, "POST 'auth/logout'": true, "GET topologyPath(route": true}
	for _, c := range calls {
		if !allowed[c[1]+" "+strings.TrimSpace(c[2])] {
			t.Errorf("the UI sends %s %s", c[1], c[2])
		}
	}
	for _, word := range []string{"'PUT'", "'DELETE'", "'PATCH'", "/deploy", "/start", "/stop", "/undeploy", "/messages", "/reprocess"} {
		if strings.Contains(js, word) {
			t.Errorf("app.js mentions %s", word)
		}
	}
}

// ui loads app.js without a browser and returns its exported functions.
func ui(t *testing.T) (*goja.Runtime, *goja.Object) {
	t.Helper()
	vm := goja.New()
	if _, err := vm.RunString(file(t, "app.js")); err != nil {
		t.Fatal(err)
	}
	return vm, vm.Get("WeavsterUI").ToObject(vm)
}

func call(t *testing.T, vm *goja.Runtime, o *goja.Object, fn string, args ...any) goja.Value {
	t.Helper()
	f, ok := goja.AssertFunction(o.Get(fn))
	if !ok {
		t.Fatalf("%s is not a function", fn)
	}
	vals := make([]goja.Value, len(args))
	for i, a := range args {
		vals[i] = vm.ToValue(a)
	}
	v, err := f(goja.Undefined(), vals...)
	if err != nil {
		t.Fatalf("%s: %v", fn, err)
	}
	return v
}

// graph parses a topology payload into a JS value.
func graph(t *testing.T, vm *goja.Runtime, json string) goja.Value {
	t.Helper()
	v, err := vm.RunString("(" + json + ")")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

const overview = `{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z","nodes":[
 {"id":"flow:adt","kind":"flow","label":"ADT <b>inbound</b>","status":"started","activity":{"received":3,"sent":2,"errored":1,"queued":0}},
 {"id":"flow:billing","kind":"flow","label":"billing","status":"errored"}],
 "edges":[{"id":"edge:flow:adt:route:flow:billing","from":"flow:adt","to":"flow:billing","kind":"route","status":"active"}]}`

const drill = `{"schemaVersion":"1","generatedAt":"2026-09-29T10:00:00Z","flowId":"flow:adt","flowName":"ADT","flowStatus":"started","nodes":[
 {"id":"source:http","kind":"source","label":"http://:9001/adt","status":"started"},
 {"id":"transform:dsl:t","kind":"transform","label":"t","status":"started"},
 {"id":"destination:a","kind":"destination","label":"a","status":"errored"},
 {"id":"destination:b","kind":"destination","label":"b","status":"stopped"},
 {"id":"flow:billing","kind":"flow","label":"billing"}],
 "edges":[
 {"id":"e1","from":"source:http","to":"transform:dsl:t","kind":"message-path","status":"active"},
 {"id":"e2","from":"transform:dsl:t","to":"destination:a","kind":"message-path","status":"errored"},
 {"id":"e3","from":"transform:dsl:t","to":"destination:b","kind":"message-path","status":"idle"},
 {"id":"e4","from":"destination:b","to":"flow:billing","kind":"route"},
 {"id":"e5","from":"flow:billing","to":"source:http","kind":"route"}]}`

// TestRouting: hashes pick the overview or one flow, and map to the API.
func TestRouting(t *testing.T) {
	vm, o := ui(t)
	for hash, want := range map[string]string{"": "topology", "#/": "topology", "#/flows/adt": "topology/flows/adt", "#/flows/a%2Fb": "topology/flows/a%2Fb"} {
		route := call(t, vm, o, "parseRoute", hash)
		if got := call(t, vm, o, "topologyPath", route).String(); got != want {
			t.Errorf("%q -> %s, want %s", hash, got, want)
		}
	}
	if got := call(t, vm, o, "flowLink", "flow:adt").String(); got != "#/flows/adt" {
		t.Errorf("flowLink = %s", got)
	}
}

// TestLayout: nodes are placed in columns along the edges, each at its own
// place; a cycle does not loop forever; dependencies do not rank.
func TestLayout(t *testing.T) {
	vm, o := ui(t)
	l := call(t, vm, o, "layout", graph(t, vm, drill)).ToObject(vm)
	pos := l.Get("pos").ToObject(vm)
	x := func(id string) int64 { return pos.Get(id).ToObject(vm).Get("x").ToInteger() }
	if x("source:http") >= x("transform:dsl:t") || x("transform:dsl:t") >= x("destination:a") || x("destination:a") != x("destination:b") || x("destination:b") >= x("flow:billing") {
		t.Errorf("columns: source %d transform %d a %d b %d billing %d", x("source:http"), x("transform:dsl:t"), x("destination:a"), x("destination:b"), x("flow:billing"))
	}
	seen := map[string]bool{}
	for _, id := range []string{"source:http", "transform:dsl:t", "destination:a", "destination:b", "flow:billing"} {
		p := pos.Get(id).ToObject(vm)
		key := p.Get("x").String() + "," + p.Get("y").String()
		if seen[key] {
			t.Errorf("two nodes at %s", key)
		}
		seen[key] = true
	}
	dep := call(t, vm, o, "layout", graph(t, vm, `{"nodes":[{"id":"flow:a"},{"id":"flow:b"}],"edges":[{"from":"flow:a","to":"flow:b","kind":"dependency"}]}`)).ToObject(vm)
	if a, b := dep.Get("pos").ToObject(vm).Get("flow:a").ToObject(vm).Get("x").ToInteger(), dep.Get("pos").ToObject(vm).Get("flow:b").ToObject(vm).Get("x").ToInteger(); a != b {
		t.Errorf("a dependency moved a column: %d %d", a, b)
	}
	if w := call(t, vm, o, "layout", graph(t, vm, `{"nodes":[],"edges":[]}`)).ToObject(vm).Get("width").ToInteger(); w != 0 {
		t.Errorf("empty width = %d", w)
	}
}

// TestRenderStates: loading, error, empty, and graph states; labels are
// escaped; flow nodes link to their drill-down; statuses become classes.
func TestRenderStates(t *testing.T) {
	vm, o := ui(t)
	state := func(js string) goja.Value { return graph(t, vm, js) }
	for _, tt := range []struct {
		name, state string
		want, not   []string
	}{
		{"loading", `{"phase":"loading","route":{"view":"overview"}}`, []string{"Loading…"}, []string{"<svg"}},
		{"error", `{"phase":"error","route":{"view":"flow","id":"x"},"error":"There is no flow <x>."}`, []string{`role="alert"`, "There is no flow &lt;x&gt;.", "All flows"}, []string{"<x>"}},
		{"no flows", `{"phase":"ready","route":{"view":"overview"},"graph":{"nodes":[],"edges":[],"generatedAt":"t"}}`, []string{"No flows yet"}, []string{"<svg"}},
		{"empty flow", `{"phase":"ready","route":{"view":"flow","id":"a"},"graph":{"flowId":"flow:a","nodes":[],"edges":[],"generatedAt":"t"}}`, []string{"no source, transform, or destinations"}, nil},
		{"overview", `{"phase":"ready","route":{"view":"overview"},"graph":` + overview + `,"updated":"10:00"}`,
			[]string{"<svg", `href="#/flows/adt"`, `href="#/flows/billing"`, "ADT &lt;b&gt;inbound&lt;/b&gt;", "status-errored", "3 in · 2 sent · 1 err · 0 queued", "kind-route status-active", "refreshed every 5 s"},
			[]string{"<b>inbound"}},
		{"drill-down", `{"phase":"ready","route":{"view":"flow","id":"adt"},"graph":` + drill + `}`,
			[]string{"<h2>ADT</h2>", "All flows", "status-stopped", "kind-message-path status-errored", `href="#/flows/billing"`},
			[]string{`href="#/flows/adt"`}},
	} {
		html := call(t, vm, o, "renderView", state(tt.state)).String()
		for _, w := range tt.want {
			if !strings.Contains(html, w) {
				t.Errorf("%s: missing %q in %s", tt.name, w, html)
			}
		}
		for _, n := range tt.not {
			if strings.Contains(html, n) {
				t.Errorf("%s: unexpected %q", tt.name, n)
			}
		}
	}
	if login := call(t, vm, o, "loginView", "Wrong <password>").String(); !strings.Contains(login, "Wrong &lt;password&gt;") || !strings.Contains(login, `type="password"`) {
		t.Errorf("login = %s", login)
	}
	if login := call(t, vm, o, "loginView", "").String(); strings.Contains(login, "alert") {
		t.Errorf("login without a message = %s", login)
	}
}

// TestErrorText: failed requests are explained.
func TestErrorText(t *testing.T) {
	vm, o := ui(t)
	for _, tt := range []struct {
		status      int
		route, body string
		want        string
	}{
		{403, `{"view":"overview"}`, `{"error":{"code":"PASSWORD_CHANGE_REQUIRED"}}`, "Change your password first"},
		{403, `{"view":"overview"}`, `{"error":{"code":"FORBIDDEN"}}`, "flows:view"},
		{404, `{"view":"flow","id":"x"}`, `null`, "There is no flow x."},
		{500, `{"view":"overview"}`, `{"error":{"message":"store down"}}`, "The server answered 500: store down."},
		{502, `{"view":"overview"}`, `null`, "The server answered 502."},
	} {
		if got := call(t, vm, o, "errorText", tt.status, graph(t, vm, tt.route), graph(t, vm, tt.body)).String(); got != tt.want && !strings.Contains(got, tt.want) {
			t.Errorf("%d = %q, want %q", tt.status, got, tt.want)
		}
	}
}
