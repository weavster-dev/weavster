package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHL7Input: a flow with inputFormat hl7v2 transforms HL7 v2 messages
// received over MLLP (and the API) with DSL paths such as PID.5.1, and
// delivers the result as JSON; a message that is not HL7 is refused.
func TestHL7Input(t *testing.T) {
	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"adt","inputFormat":"hl7v2","source":{"type":"mllp","address":"`+src+`"},`+
		`"transform":{"name":"t","steps":[`+
		`{"filter":{"when":"MSH.9.2 == 'A01'","action":"accept"}},`+
		`{"map":{"from":"PID.5.1","to":"patient.last"}},`+
		`{"map":{"from":"PID.3.repetitions.1.1","to":"patient.ssn"}}]},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)

	m := dialMLLP(t, src)
	defer func() { _ = m.conn.Close() }()
	a01 := "MSH|^~\\&|LAB|HOSP|W|H|20260927120000||ADT^A01|MSG1|P|2.5\rPID|1||123^^^MRN~456^^^SSN||DOE^JOHN\r"
	if ack := m.send(a01); ack[1] != "MSA|AA|MSG1" {
		t.Errorf("ACK = %q", ack)
	}
	a08 := strings.Replace(strings.Replace(a01, "ADT^A01", "ADT^A08", 1), "MSG1", "MSG2", 1)
	if ack := m.send(a08); ack[1] != "MSA|AA|MSG2" { // stored, then filtered by the flow
		t.Errorf("ACK = %q", ack)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("delivered %d files, want 1 (A08 filtered)", len(entries))
	}
	body, _ := os.ReadFile(filepath.Join(out, entries[0].Name()))
	if !strings.Contains(string(body), `"patient":{"last":"DOE","ssn":"456"}`) {
		t.Errorf("delivered %s", body)
	}

	// Over the API, JSON is refused by an hl7v2 flow.
	code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/adt/messages", `{"PID":{"5":{"1":"Doe"}}}`, admin)
	if code != http.StatusBadRequest || !strings.Contains(resp, "body must be an HL7 v2 message") {
		t.Errorf("JSON to an hl7v2 flow: %d %s", code, resp)
	}
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"bad","inputFormat":"csv"}`, admin); code != http.StatusBadRequest {
		t.Errorf("unknown inputFormat: %d %s", code, resp)
	}
}
