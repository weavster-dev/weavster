package main

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestXMLResponses: a client that explicitly prefers XML gets every API
// response, errors included, as XML; JSON stays the default (browsers
// included), and archives, the OpenAPI document, and message content are
// unchanged.
func TestXMLResponses(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"adt","name":"ADT Inbound","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
	c.do(http.MethodPost, "/api/v1/lookups/codes/import", `{"ICD/10":"x"}`, admin)
	id, _ := sendMessage(t, c, "adt", `{"k":"v"}`)
	accept := func(value string) func(*http.Request) {
		return func(r *http.Request) { admin(r); r.Header.Set("Accept", value) }
	}
	for _, tt := range []struct {
		name, method, path, accept string
		status                     int
		contentType, want          string
	}{
		{"list", http.MethodGet, "/api/v1/flows", "application/xml", http.StatusOK, "application/xml; charset=utf-8",
			`<response type="array"><item><id>adt</id><name>ADT Inbound</name>`},
		{"object", http.MethodGet, "/api/v1/flows/adt/stats", "text/xml", http.StatusOK, "application/xml; charset=utf-8", `<received type="number">1</received>`},
		{"map with keys that are not names", http.MethodGet, "/api/v1/lookups/codes", "application/xml", http.StatusOK, "application/xml; charset=utf-8",
			`<response><entry key="ICD/10">x</entry></response>`},
		{"error envelope", http.MethodGet, "/api/v1/flows/nope", "application/xml", http.StatusNotFound, "application/xml; charset=utf-8",
			`<response><error><code>NOT_FOUND</code><message>flow not found</message></error></response>`},
		{"json preferred", http.MethodGet, "/api/v1/flows", "application/json, application/xml;q=0.9", http.StatusOK, "application/json", `[{"id":"adt"`},
		{"default", http.MethodGet, "/api/v1/flows", "", http.StatusOK, "application/json", `[{"id":"adt"`},
		{"browser", http.MethodGet, "/api/v1/flows", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", http.StatusOK, "application/json", `[{"id":"adt"`},
		{"message content unchanged", http.MethodGet, "/api/v1/messages/" + id + "/content", "application/xml", http.StatusOK, "", `{"k":"v"}`},
		{"archive unchanged", http.MethodGet, "/api/v1/messages/export", "application/xml", http.StatusOK, "application/gzip", ""},
		{"openapi unchanged", http.MethodGet, "/api/openapi.yaml", "application/xml", http.StatusOK, "application/yaml", "openapi:"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, body, h := c.do(tt.method, tt.path, "", accept(tt.accept))
			if code != tt.status || !strings.HasPrefix(h.Get("Content-Type"), tt.contentType) || !strings.Contains(body, tt.want) {
				t.Errorf("%d %s %.300q", code, h.Get("Content-Type"), body)
			}
			if tt.contentType == "" && body != tt.want {
				t.Errorf("content = %q", body)
			}
			if strings.HasPrefix(tt.contentType, "application/xml") {
				if err := xml.Unmarshal([]byte(body), new(struct{})); err != nil {
					t.Errorf("not well-formed XML: %v", err)
				}
			}
		})
	}
	// An events export served as XML is named events.xml.
	for value, name := range map[string]string{"application/xml": "events.xml", "": "events.json"} {
		if code, _, h := c.do(http.MethodGet, "/api/v1/events/export", "", accept(value)); code != http.StatusOK ||
			!strings.Contains(h.Get("Content-Disposition"), `filename="`+name+`"`) {
			t.Errorf("export %q: %d %v", value, code, h)
		}
	}
}
