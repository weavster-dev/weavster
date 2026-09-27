package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONToXML(t *testing.T) {
	for _, tt := range []struct{ name, in, want string }{
		{"object keeps key order", `{"b":1,"a":"x"}`, `<response><b type="number">1</b><a>x</a></response>`},
		{"array", `[1,"two",true,null]`, `<response type="array"><item type="number">1</item><item>two</item><item type="boolean">true</item><item type="null"></item></response>`},
		{"empty array and object", `{"list":[],"map":{}}`, `<response><list type="array"></list><map></map></response>`},
		{"keys that are not names", `{"A/B":"x","1st":"y","xmlish":"z"}`, `<response><entry key="A/B">x</entry><entry key="1st">y</entry><entry key="xmlish">z</entry></response>`},
		{"escaping", `{"code":"a < b && c","k":{"q\"":"&"}}`, `<response><code>a &lt; b &amp;&amp; c</code><k><entry key="q&#34;">&amp;</entry></k></response>`},
		{"nested", `{"error":{"code":"NOT_FOUND","message":"flow not found"}}`, `<response><error><code>NOT_FOUND</code><message>flow not found</message></error></response>`},
		{"exact number", `{"n":9007199254740993}`, `<response><n type="number">9007199254740993</n></response>`},
	} {
		got, err := jsonToXML([]byte(tt.in))
		if err != nil || strings.TrimSpace(strings.TrimPrefix(string(got), `<?xml version="1.0" encoding="UTF-8"?>`+"\n")) != tt.want {
			t.Errorf("%s: %s, %v", tt.name, got, err)
		}
	}
	for _, bad := range []string{`{`, `{"a":1} {}`, `[1,`} {
		if _, err := jsonToXML([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestPrefersXML(t *testing.T) {
	for accept, want := range map[string]bool{
		"": false, "*/*": false, "application/json": false, "application/xml": true, "text/xml": true,
		"application/json, application/xml": false, "application/xml, application/json": false,
		"application/json;q=0.5, application/xml": true, "application/xml;q=0, */*": false,
		"*/*;q=0.1, text/xml": true, "text/html, application/xml;q=0.9": true, "bad;;;, application/xml": true,
	} {
		if got := prefersXML(accept); got != want {
			t.Errorf("%q = %v, want %v", accept, got, want)
		}
	}
}

func TestNegotiateXML(t *testing.T) {
	serve := func(h http.HandlerFunc, accept string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		negotiateXML(h).ServeHTTP(rec, req)
		return rec
	}
	jsonH := func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]int{"n": 1}) }
	if rec := serve(jsonH, "application/xml"); rec.Code != http.StatusCreated || rec.Header().Get("Content-Type") != "application/xml; charset=utf-8" ||
		!strings.Contains(rec.Body.String(), `<n type="number">1</n>`) || rec.Header().Get("Vary") != "Accept" {
		t.Errorf("xml = %d %v %s", rec.Code, rec.Header(), rec.Body.String())
	}
	if rec := serve(jsonH, ""); rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("default = %v", rec.Header())
	}
	gz := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write([]byte("archive"))
	}
	if rec := serve(gz, "application/xml"); rec.Body.String() != "archive" || rec.Header().Get("Content-Type") != "application/gzip" {
		t.Errorf("non-JSON = %v %s", rec.Header(), rec.Body.String())
	}
	broken := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not json"))
	}
	if rec := serve(broken, "application/xml"); rec.Body.String() != "{not json" || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("unconvertible = %v %s", rec.Header(), rec.Body.String())
	}
	twice := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.WriteHeader(http.StatusTeapot) // ignored, as net/http does
		_, _ = w.Write([]byte(`{}`))
	}
	if rec := serve(twice, "application/xml"); rec.Code != http.StatusAccepted {
		t.Errorf("status = %d", rec.Code)
	}
}
