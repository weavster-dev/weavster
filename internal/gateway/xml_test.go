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
		{"empty array and object", `{"list":[],"map":{}}`, `<response><list type="array"></list><map type="object"></map></response>`},
		{"empty key and string", `{"":"","s":""}`, `<response><entry key=""></entry><s></s></response>`},
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
	for _, tt := range []struct {
		accept string
		want   bool
	}{
		{"", false},
		{"*/*", false},
		{"application/json", false},
		{"application/xml", true},
		{"text/xml", true},
		{"application/json, application/xml", false},
		{"application/xml, application/json", false},
		{"application/json;q=0.5, application/xml", true},
		{"application/xml;q=0, */*", false},
		{"*/*;q=0.1, text/xml", true},
		{"application/xml, */*", false}, // JSON matches */* at the same quality
		{"text/html, application/xhtml+xml, application/xml;q=0.9, */*;q=0.8", false}, // a browser
		{"application/xml;q=0.9, */*;q=0.8", true},
		{"application/json;q=0.1, */*, application/xml;q=0.5", false}, // */* does not raise JSON above 0.1, but XML is not top
		{"application/json;q=0.1, application/xml;q=0.5", true},       // the specific JSON range wins over */*
		{"application/json;q=0.1, */*;q=0.9, application/xml;q=0.5", false},
		{"application/*;q=0.2, application/xml;q=0.4", true},
		{"application/xml;q=abc", false},
		{"application/xml;q=2", false},
		{"application/xml;q=-1", false},
		{"application/xml, text/html;q=NaN", true},
		{"bad;;;, application/xml", true},
	} {
		if got := prefersXML(tt.accept); got != tt.want {
			t.Errorf("%q = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

func TestNegotiateXML(t *testing.T) {
	respond := func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusCreated, map[string]int{"n": 1}) }
	content := func(w http.ResponseWriter, _ *http.Request) { // stored message content
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"raw":true}`))
	}
	recorded := func(w http.ResponseWriter, r *http.Request) { respond(&statusRecorder{ResponseWriter: w}, r) }
	unencodable := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"c": make(chan int)})
	}
	for _, tt := range []struct {
		name     string
		h        http.HandlerFunc
		accept   []string
		ctype    string
		contains string
	}{
		{"xml", respond, []string{"application/xml"}, "application/xml; charset=utf-8", `<n type="number">1</n>`},
		{"json by default", respond, nil, "application/json", `{"n":1}`},
		{"second header line", respond, []string{"text/html;q=0.1", "application/xml"}, "application/xml; charset=utf-8", `<n type="number">1</n>`},
		{"through a wrapping writer", recorded, []string{"application/xml"}, "application/xml; charset=utf-8", `<n type="number">1</n>`},
		{"content is never converted", content, []string{"application/xml"}, "application/json", `{"raw":true}`},
		{"unencodable falls back", unencodable, []string{"application/xml"}, "application/json", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for _, a := range tt.accept {
				req.Header.Add("Accept", a)
			}
			negotiateXML(tt.h).ServeHTTP(rec, req)
			if rec.Header().Get("Content-Type") != tt.ctype || !strings.Contains(rec.Body.String(), tt.contains) || rec.Header().Get("Vary") != "Accept" {
				t.Errorf("got %d %v %s", rec.Code, rec.Header(), rec.Body.String())
			}
		})
	}
}
