package codecs

import (
	"strings"
	"testing"
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
	if !strings.Contains(ack, "|ACK|") {
		t.Errorf("expected MSH-9 ACK message type, got %q", ack)
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
