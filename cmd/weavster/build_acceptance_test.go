package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBuildOutput: HL7 v2 in over MLLP, a flow transform that rebuilds it
// as HL7 v2 (values escaped) sent on over MLLP, and a destination that
// builds XML for an HTTP receiver.
func TestBuildOutput(t *testing.T) {
	var mu sync.Mutex
	var xmlIn string
	ehr := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		xmlIn = r.Header.Get("Content-Type") + " " + string(b)
		mu.Unlock()
	}))
	defer ehr.Close()
	lab := &mllpReceiver{codes: []string{"AA"}}
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	createFlow(t, c, `{"id":"relay","inputFormat":"hl7v2","source":{"type":"mllp","address":"`+src+`"},`+
		`"transform":{"steps":[`+
		`{"set":{"field":"name","expr":"{{PID.5.1}}^{{PID.5.2}}"}},`+
		`{"build":{"format":"hl7v2","template":"MSH|^~\\&|WEAVSTER|H|LAB|H|{{MSH.7.1}}||ADT^A08|{{MSH.10.1}}|P|2.5\nPID|1||{{PID.3.1}}^^^MRN||{{name}}"}}]},`+
		`"destinations":[{"name":"lab","type":"mllp","address":"`+lab.serve(t)+`"},`+
		`{"name":"ehr","type":"http","url":"`+ehr.URL+`","transform":{"steps":[{"build":{"format":"xml","template":"<patient mrn=\"{{PID.3.1}}\">{{PID.5.1}}</patient>"}}]}}]}`)

	m := dialMLLP(t, src)
	defer func() { _ = m.conn.Close() }()
	in := "MSH|^~\\&|ADT|H|W|H|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||123||DOE^JOHN\r"
	if ack := m.send(in); ack[1] != "MSA|AA|MSG1" {
		t.Fatalf("ACK = %q", ack)
	}
	waitStatus(t, c, firstMessageID(t, c, "relay"), "sent")
	// The ^ in the value is escaped, so the name stays one component.
	want := "MSH|^~\\&|WEAVSTER|H|LAB|H|20260927120000||ADT^A08|MSG1|P|2.5\rPID|1||123^^^MRN||DOE\\S\\JOHN\r"
	if lab.count() != 1 || lab.received[0] != want {
		t.Errorf("lab received %q\nwant %q", lab.received, want)
	}
	mu.Lock()
	defer mu.Unlock()
	// The ehr destination reads the flow's rebuilt HL7: PID.5.1 is the whole
	// escaped name, decoded again.
	if xmlIn != `application/xml <patient mrn="123">DOE^JOHN</patient>` {
		t.Errorf("ehr received %q", xmlIn)
	}

	for body, want := range map[string]string{
		`{"id":"x","transform":{"steps":[{"build":{"template":"{}"}},{"set":{"field":"a","expr":"b"}}]}}`:                                                          "build must be the last step",
		`{"id":"x","transform":{"steps":[{"build":{"format":"xml","template":"<a/>"}}]},"destinations":[{"name":"l","type":"mllp","address":"lab.example:2575"}]}`: "needs an HL7 v2 message",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}

// firstMessageID is the id of the flow's first message, waiting for it.
func firstMessageID(t *testing.T, c apiClient, flowID string) string {
	t.Helper()
	for i := 0; i < 500; i++ {
		_, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId="+flowID, "", basic(bootstrapAdmin, testAdminPassword))
		if i := strings.Index(body, `"id":"`); i >= 0 {
			rest := body[i+6:]
			return rest[:strings.Index(rest, `"`)]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no message for flow %s", flowID)
	return ""
}
