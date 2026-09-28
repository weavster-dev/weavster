package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestResponseSelector: the message request returns the selected
// destination's reply after its response transform; other destinations'
// replies are not returned.
func TestResponseSelector(t *testing.T) {
	reply := func(body string) *httptest.Server {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(s.Close)
		return s
	}
	ack, other := reply(`{"code":"AA","control":"123"}`), reply(`{"code":"other"}`)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	stop := startCLI(t, []string{"server", "--config", cfg}, c.base+"/api/openapi.yaml")
	defer stop()

	for body, want := range map[string]string{
		`{"id":"x","responseSelector":"nope","destinations":[{"name":"a","type":"http","url":"` + ack.URL + `"}]}`:                                                               `no destination named \"nope\"`, // JSON-escaped in the error envelope
		`{"id":"x","responseSelector":"a","destinations":[{"name":"a","type":"http","url":"` + ack.URL + `","responseTransform":{"steps":[{"map":{"from":"a..b","to":"c"}}]}}]}`: `invalid path \"a..b\"`,
		`{"id":"x","responseSelector":"a","destinations":[{"name":"a","type":"file","dir":"` + t.TempDir() + `"}]}`:                                                              "sends no reply",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("create %s: %d %q, want 400 with %q", body, code, resp, want)
		}
	}

	createFlow(t, c, `{"id":"f","responseSelector":"ehr","destinations":[`+
		`{"name":"archive","type":"http","url":"`+other.URL+`"},`+
		`{"name":"ehr","type":"http","url":"`+ack.URL+`","responseTransform":{"steps":[{"map":{"from":"code","to":"ack"}}]}}]}`)
	code, body, _ := c.do(http.MethodPost, "/api/v1/flows/f/messages", `{"kind":"adt"}`, admin)
	if code != http.StatusAccepted || !strings.Contains(body, `"status":"sent"`) || !strings.Contains(body, `"response":{"ack":"AA","code":"AA","control":"123"}`) {
		t.Errorf("message: %d %q", code, body)
	}

	// Without a selector no response is returned.
	createFlow(t, c, `{"id":"g","destinations":[{"name":"ehr","type":"http","url":"`+ack.URL+`"}]}`)
	if _, body, _ := c.do(http.MethodPost, "/api/v1/flows/g/messages", `x`, admin); strings.Contains(body, "response") {
		t.Errorf("flow without selector returned %q", body)
	}
}
