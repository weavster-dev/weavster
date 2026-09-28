package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDestinationSet: a flow's destinationSet step routes each message by
// its content; the exclusion is stored with the message (visible as
// metadata) and still holds after a restart; unknown names are refused.
func TestDestinationSet(t *testing.T) {
	var ehrHits atomic.Int32
	ehr := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ehrHits.Add(1) }))
	defer ehr.Close()
	archive := t.TempDir()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	args := []string{"server", "--config", cfg}
	stop := startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"route","transform":{"steps":[`+
		`{"destinationSet":{"exclude":["archive"],"when":"kind == 'orm'"}},`+
		`{"destinationSet":{"exclude":["ehr"],"when":"test"}}]},`+
		`"destinations":[{"name":"ehr","type":"http","url":"`+ehr.URL+`"},{"name":"archive","type":"file","dir":"`+archive+`"}]}`)
	files := func() int { e, _ := os.ReadDir(archive); return len(e) }

	for _, tt := range []struct {
		body, status  string
		ehr, archived int
	}{
		{`{"kind":"adt"}`, "sent", 1, 1},
		{`{"kind":"orm"}`, "sent", 2, 1},
		{`{"kind":"orm","test":true}`, "filtered", 2, 1},
	} {
		if _, status := sendMessage(t, c, "route", tt.body); status != tt.status || int(ehrHits.Load()) != tt.ehr || files() != tt.archived {
			t.Errorf("%s: %s, ehr %d, archive %d; want %s, %d, %d", tt.body, status, ehrHits.Load(), files(), tt.status, tt.ehr, tt.archived)
		}
	}

	// Hold ehr, send an ORM (archive excluded), restart, release ehr: the
	// message goes to ehr only.
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/route/destinations/ehr/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop ehr: %d %s", code, body)
	}
	id, _ := sendMessage(t, c, "route", `{"kind":"orm"}`)
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+id, "", admin); !strings.Contains(body, `"destinationSet.excluded":"archive"`) {
		t.Errorf("message = %s", body)
	}
	stop()
	stop = startCLI(t, args, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/route/destinations/ehr/start", "", admin); code != http.StatusOK {
		t.Fatalf("start ehr: %d %s", code, body)
	}
	waitStatus(t, c, id, "sent")
	if ehrHits.Load() != 3 || files() != 1 {
		t.Errorf("after restart: ehr %d, archive %d; want 3, 1", ehrHits.Load(), files())
	}

	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","transform":{"steps":[{"destinationSet":{"exclude":["nope"]}}]},"destinations":[{"name":"ehr","type":"http","url":"https://x"}]}`, admin); code != http.StatusBadRequest || !strings.Contains(body, `excludes \"nope\", which is not a destination`) {
		t.Errorf("unknown name: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","transform":{"steps":[{"destinationSet":{"include":["ehr"]}}]}}`, admin); code != http.StatusBadRequest || !strings.Contains(body, "flow.schema.json") {
		t.Errorf("include: %d %s", code, body)
	}
}
