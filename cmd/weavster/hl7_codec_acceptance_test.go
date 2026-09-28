package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHL7Codec: an hl7v2 flow reads subcomponents, repetitions, and escaped
// delimiters (also with a message's own delimiters), answers a message with
// custom delimiters with a correct ACK, passes HL7 through unchanged, and
// refuses an unsupported version.
func TestHL7Codec(t *testing.T) {
	addr, src, pass := freeAddr(t), freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+t.TempDir()+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out, raw := t.TempDir(), t.TempDir()
	createFlow(t, c, `{"id":"adt","inputFormat":"hl7v2","source":{"type":"mllp","address":"`+src+`"},`+
		`"transform":{"name":"t","steps":[`+
		`{"map":{"from":"PID.3.4.1","to":"mrn.authority"}},`+
		`{"map":{"from":"PID.3.4.2","to":"mrn.oid"}},`+
		`{"map":{"from":"PID.3.repetitions.1.1","to":"ssn"}},`+
		`{"map":{"from":"PID.5.1","to":"last"}},`+
		`{"map":{"from":"PID.5.2","to":"first"}}]},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)

	m := dialMLLP(t, src)
	defer func() { _ = m.conn.Close() }()
	for _, tt := range []struct{ name, msg, ack string }{
		{"standard", "MSH|^~\\&|LAB|HOSP|W|H|20260927120000||ADT^A01|M1|P|2.5.1\r" +
			"PID|1||123^^^HOSP&1.2.3&ISO~456^^^SSN||O\\T\\BRIEN^ANN\\S\\MARIE\r", "MSA|AA|M1"},
		{"custom delimiters", "MSH#$%!@#LAB#HOSP#W#H#20260927120000##ADT$A01#M!F!2#P#2.4\r" +
			"PID#1##123$$$HOSP@1.2.3@ISO%456$$$SSN##O&BRIEN$ANN^MARIE\r", "MSA|AA|M#2"},
	} {
		if ack := m.send(tt.msg); len(ack) != 2 || ack[1] != tt.ack || !strings.HasPrefix(ack[0], "MSH|^~\\&|W|H|LAB|HOSP|") {
			t.Errorf("%s: ACK = %q", tt.name, ack)
		}
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=adt", "", admin); !strings.Contains(body, `"source.mllp.controlId":"M#2"`) {
		t.Errorf("the control id metadata is not decoded: %s", body)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 2 {
		t.Fatalf("delivered %d files, want 2", len(entries))
	}
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(out, e.Name()))
		var doc struct {
			First, Last, SSN string
			MRN              struct{ Authority, OID string }
		}
		if err := json.Unmarshal(body, &doc); err != nil || doc.First != "ANN^MARIE" || doc.Last != "O&BRIEN" || doc.SSN != "456" ||
			doc.MRN.Authority != "HOSP" || doc.MRN.OID != "1.2.3" {
			t.Errorf("delivered %s (%v)", body, err)
		}
	}

	// An unsupported version is refused, over the API and over MLLP (AR).
	v3 := "MSH|^~\\&|LAB|HOSP|W|H|20260927120000||ADT^A01|M3|P|3.0\rPID|1\r"
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/adt/messages", v3, admin); code != http.StatusBadRequest || !strings.Contains(resp, "unsupported HL7 version (MSH-12 must be 2.1 to 2.9)") {
		t.Errorf("version 3.0: %d %s", code, resp)
	}
	if ack := m.send(v3); len(ack) != 2 || !strings.HasPrefix(ack[1], "MSA|AR|M3|") {
		t.Errorf("version 3.0 over MLLP: ACK = %q", ack)
	}

	// Without transforms the message is delivered byte for byte.
	createFlow(t, c, `{"id":"pass","source":{"type":"mllp","address":"`+pass+`"},"destinations":[{"name":"out","type":"file","dir":"`+raw+`"}]}`)
	p := dialMLLP(t, pass)
	defer func() { _ = p.conn.Close() }()
	msg := "MSH#$%!@#LAB#HOSP#W#H#20260927120000##ADT$A01#P1#P#2.4\rPID#1##1$$$H@O!T!X\r"
	if ack := p.send(msg); len(ack) != 2 || ack[1] != "MSA|AA|P1" {
		t.Errorf("passthrough ACK = %q", ack)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		files, _ := os.ReadDir(raw)
		if len(files) == 1 {
			if body, _ := os.ReadFile(filepath.Join(raw, files[0].Name())); string(body) != msg {
				t.Errorf("passthrough delivered %q", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("passthrough message not delivered")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
