package codecs

import (
	"strings"
	"testing"
	"time"
)

func TestRegistry(t *testing.T) {
	r := Standard()
	for _, name := range []string{"json", "xml", "raw", "delimited", "hl7v2", "x12", "ncpdp"} {
		if _, err := r.Get(name); err != nil {
			t.Errorf("expected codec %q registered, got %v", name, err)
		}
	}
	if _, err := r.Get("does-not-exist"); err == nil {
		t.Error("expected error for unknown codec")
	}
}

func TestRoundTrips(t *testing.T) {
	tests := []struct {
		name string
		c    Codec
		in   []byte
	}{
		{"json", JSON(), []byte(`{"a":1,"b":[true,null,"x"]}`)},
		{"xml", XML(), []byte(`<root attr="v"><child>text</child></root>`)},
		{"raw", Raw(), []byte{0x00, 0x01, 0xff, 0x02}},
		{"delimited", NewDelimited('|', true), []byte("a|b|c\n1|2|3\n")},
		{"hl7v2", HL7v2(), []byte("MSH|^~\\&|SENDAPP|SENDFAC|RECVAPP|RECVFAC|20240101120000||ADT^A01|MSG0001|P|2.5\rPID|1||12345^^^MRN~67890^^^ALT||DOE^JOHN\r")},
		{"x12", X12(), []byte("ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*U*00401*000000001*0*P*>~\nGS*HS*SENDER*RECEIVER*20240101*1200*1*X*004010~\nST*270*0001~\nSE*3*0001~\nGE*1*1~\nIEA*1*000000001~\n")},
		{"ncpdp", NCPDP(), []byte("00\x1cT1\x1c1234\x1e")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.c.Parse(tc.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			out, err := tc.c.Serialize(v)
			if err != nil {
				t.Fatalf("serialize: %v", err)
			}
			if _, err := tc.c.Parse(out); err != nil {
				t.Fatalf("re-parse of serialized output failed: %v", err)
			}
		})
	}
}

func TestHL7Ack(t *testing.T) {
	c := HL7v2()
	in := []byte("MSH|^~\\&|SENDAPP|SENDFAC|RECVAPP|RECVFAC|20240101120000||ADT^A01|MSG0001|P|2.5\rPID|1||12345||DOE^JOHN\r")
	ackBytes, err := c.Acknowledge(in)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	ack := string(ackBytes)
	if !strings.Contains(ack, "MSA|AA|MSG0001") {
		t.Errorf("expected MSA with AA and echoed control id, got %q", ack)
	}
	if !strings.Contains(ack, "|ACK^A01|") {
		t.Errorf("expected MSH-9 ACK^A01 message type, got %q", ack)
	}
}

// TestHL7ACKOptions: the ACK carries the given code, text, its own control
// id, and the time it was made; a message without MSH still gets one.
func TestHL7ACKOptions(t *testing.T) {
	now := time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC)
	in := []byte("MSH|^~\\&|SENDAPP|SENDFAC|RECVAPP|RECVFAC|20240101120000||ADT^A01|MSG0001|P|2.5\rPID|1||12345||DOE^JOHN\r")
	if ack, _ := HL7ACK(in, HL7AckOptions{Code: AckApplicationAccept}); strings.Contains(string(ack), "|00010101") {
		t.Errorf("zero Now gave %q", ack)
	}
	for _, tt := range []struct {
		name string
		in   []byte
		opts HL7AckOptions
		want string
	}{
		{"accept", in, HL7AckOptions{Code: AckApplicationAccept, ControlID: "A1", Now: now},
			"MSH|^~\\&|RECVAPP|RECVFAC|SENDAPP|SENDFAC|20260927010203||ACK^A01|A1|P|2.5\rMSA|AA|MSG0001\r"},
		{"error with text", in, HL7AckOptions{Code: AckApplicationError, Text: "flow is not accepting messages", Now: now},
			"MSH|^~\\&|RECVAPP|RECVFAC|SENDAPP|SENDFAC|20260927010203||ACK^A01|MSG0001|P|2.5\rMSA|AE|MSG0001|flow is not accepting messages\r"},
		{"no MSH", []byte("not hl7"), HL7AckOptions{Code: AckApplicationReject, Text: "not an HL7 v2 message", Now: now},
			"MSH|^~\\&|||||20260927010203||ACK|||\rMSA|AR||not an HL7 v2 message\r"},
	} {
		got, err := HL7ACK(tt.in, tt.opts)
		if err != nil || string(got) != tt.want {
			t.Errorf("%s: %q, %v\nwant %q", tt.name, got, err, tt.want)
		}
	}
}

// TestHL7Delimiters: components and repetitions follow MSH-2, standard or
// not.
func TestHL7Delimiters(t *testing.T) {
	for _, tt := range []struct {
		name, in string
	}{
		{"standard", "MSH|^~\\&|A|B|C|D|20240101||ADT^A01|1|P|2.5\rPID|1||42^^^H~43^^^I\r"},
		{"custom", "MSH|#!\\*|A|B|C|D|20240101||ADT#A01|1|P|2.5\rPID|1||42###H!43###I\r"},
	} {
		v, err := HL7v2().Parse([]byte(tt.in))
		if err != nil {
			t.Fatal(err)
		}
		m := v.(*HL7Message)
		if typ := m.Segments[0].Field(9); len(typ) != 2 || typ[1] != "A01" {
			t.Errorf("%s: MSH-9 = %q", tt.name, typ)
		}
		if ids := m.Segments[1].Fields[2]; len(ids) != 2 || ids[1][0] != "43" || ids[1][3] != "I" {
			t.Errorf("%s: PID-3 = %q", tt.name, ids)
		}
	}
}

func TestX12Ack997(t *testing.T) {
	c := X12()
	in := []byte("ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*U*00401*000000001*0*P*>~\nGS*HS*SENDER*RECEIVER*20240101*1200*1*X*004010~\nST*270*0042~\nSE*3*0042~\nGE*1*1~\nIEA*1*000000001~\n")
	ackBytes, err := c.Acknowledge(in)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	ack := string(ackBytes)
	if !strings.Contains(ack, "ST*997*0042") {
		t.Errorf("expected 997 ack echoing ST control, got %q", ack)
	}
	if !strings.Contains(ack, "AK9*A") {
		t.Errorf("expected AK9 accept, got %q", ack)
	}
}

func TestXMLXXESafe(t *testing.T) {
	c := XML()
	in := []byte("<?xml version=\"1.0\"?><!DOCTYPE note [<!ENTITY xxe SYSTEM \"file:///etc/passwd\">]><note>&xxe;</note>")
	v, err := c.Parse(in)
	if err == nil {
		// Go's decoder does not resolve the external entity; ensure no leakage.
		out, serr := c.Serialize(v)
		if serr == nil && strings.Contains(string(out), "root:") {
			t.Errorf("XXE: external entity content leaked into output: %s", out)
		}
		return
	}
	// An error (unknown entity) is also acceptable: no resolution occurred.
	if strings.Contains(err.Error(), "file:///etc/passwd") {
		t.Errorf("unexpected external resolution: %v", err)
	}
}

func TestDICOMEnterprise(t *testing.T) {
	c := DICOM()
	if _, err := c.Parse(nil); err != ErrEnterprise {
		t.Errorf("expected ErrEnterprise, got %v", err)
	}
	if _, err := c.Serialize(nil); err != ErrEnterprise {
		t.Errorf("expected ErrEnterprise, got %v", err)
	}
}

func TestNCPDPAmounts(t *testing.T) {
	tests := []struct {
		amount string
		width  int
		want   string
	}{
		{"12.34", 6, "001234"},
		{"0.50", 6, "000050"},
		{"1", 4, "0100"},
	}
	for _, tc := range tests {
		got, err := FormatAmount(tc.amount, tc.width)
		if err != nil {
			t.Fatalf("FormatAmount(%q): %v", tc.amount, err)
		}
		if got != tc.want {
			t.Errorf("FormatAmount(%q, %d) = %q, want %q", tc.amount, tc.width, got, tc.want)
		}
		back, err := ParseAmount(got)
		if err != nil {
			t.Fatalf("ParseAmount(%q): %v", got, err)
		}
		if back != "12.34" && tc.amount == "12.34" {
			t.Errorf("round-trip ParseAmount(%q) = %q, want 12.34", got, back)
		}
	}
}

func TestCoverageMatrix(t *testing.T) {
	m := CoverageMatrix()
	if len(m) == 0 {
		t.Fatal("expected non-empty coverage matrix")
	}
	seen := map[string]bool{}
	for _, e := range m {
		if seen[e.Name] {
			t.Errorf("duplicate coverage entry %q", e.Name)
		}
		seen[e.Name] = true
	}
}

func TestParseHL7ACKAndControlID(t *testing.T) {
	for _, tt := range []struct {
		in       string
		code, id string
		ok       bool
	}{
		{"MSH|^~\\&|A|B|C|D|1||ACK^A01|X|P|2.5\rMSA|AA|MSG1\r", "AA", "MSG1", true},
		{"MSH|^~\\&|A\rMSA|CE|MSG2|busy\r", "CE", "MSG2", true},
		{"MSH|^~\\&|A\rMSA|AR\r", "AR", "", true},
		{"MSH|^~\\&|A\r", "", "", false},
		{"MSH|^~\\&|A\rMSA||MSG1\r", "", "", false},
		{"hello", "", "", false},
	} {
		code, id, ok := ParseHL7ACK([]byte(tt.in))
		if code != tt.code || id != tt.id || ok != tt.ok {
			t.Errorf("%q: %q %q %v", tt.in, code, id, ok)
		}
	}
	if got := HL7ControlID([]byte("MSH|^~\\&|A|B|C|D|1||ADT^A01|C9|P|2.5\r")); got != "C9" {
		t.Errorf("control id = %q", got)
	}
	if got := HL7ControlID([]byte("PID|1\r")); got != "" {
		t.Errorf("control id without MSH = %q", got)
	}
}
