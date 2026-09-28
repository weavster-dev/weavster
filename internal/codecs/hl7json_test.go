package codecs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHL7JSON(t *testing.T) {
	msg := "MSH|^~\\&|LAB|HOSP|W|H|20260927120000||ORU^R01|C1|P|2.5\r" +
		"PID|1||123^^^MRN~456^^^SSN||DOE^JOHN^^^DR||19800101|M\r" +
		"OBX|1|NM|GLU^Glucose||5.4|mmol/L\r" +
		"OBX|2|ST|NOTE||a \\F\\ b\r"
	doc, err := HL7JSON([]byte(msg))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(doc)
	var back map[string]any
	_ = json.Unmarshal(got, &back)
	for path, want := range map[string]string{
		`MSH.2.1`:               "^",
		`MSH.9.1`:               "ORU",
		`MSH.9.2`:               "R01",
		`MSH.10.1`:              "C1",
		`PID.3.1`:               "123",
		`PID.3.4`:               "MRN",
		`PID.3.repetitions.1.1`: "456",
		`PID.5.1`:               "DOE",
		`PID.5.5`:               "DR",
		`PID.8.1`:               "M",
		`OBX.5.1`:               "5.4",
		`segments.3.5.1`:        "a | b",
		`segments.3.name`:       "OBX",
		`segments.0.name`:       "MSH",
	} {
		if v := lookup(back, path); v != want {
			t.Errorf("%s = %v, want %q", path, v, want)
		}
	}
	for _, missing := range []string{"PID.2", "PID.5.3", "PID.5.repetitions", "MSH.1"} {
		if v := lookup(back, missing); v != nil {
			t.Errorf("%s = %v, want absent", missing, v)
		}
	}
	// Empty repetitions keep their place; escapes use the message's own
	// delimiters.
	doc, err = HL7JSON([]byte("MSH#^~!&#A####1##ADT^A01#C1\rPID#1##~456^^^SSN~~789#a !F! b !T! c\r"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ = json.Marshal(doc)
	back = nil
	_ = json.Unmarshal(got, &back)
	for path, want := range map[string]any{
		`PID.3.1`:               nil,
		`PID.3.repetitions.1.1`: "456",
		`PID.3.repetitions.3.1`: "789",
		`PID.4.1`:               "a # b & c",
	} {
		if v := lookup(back, path); v != want {
			t.Errorf("%s = %v, want %v", path, v, want)
		}
	}
	if reps, _ := lookup(back, "PID.3.repetitions").([]any); len(reps) != 4 {
		t.Errorf("PID.3.repetitions = %v, want 4 in place", reps)
	}
	for _, bad := range []string{"", "PID|1\r", "hello", "MS", "MSH"} {
		if _, err := HL7JSON([]byte(bad)); !errors.Is(err, ErrNotHL7) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if _, err := HL7JSON([]byte("\r\nMSH|^~\\&|A\r")); err != nil {
		t.Errorf("line break before MSH: %v", err)
	}
}

// lookup follows a dot path through objects and arrays.
func lookup(v any, path string) any {
	cur := v
	start := 0
	for i := 0; i <= len(path); i++ {
		if i < len(path) && path[i] != '.' {
			continue
		}
		key := path[start:i]
		start = i + 1
		switch c := cur.(type) {
		case map[string]any:
			cur = c[key]
		case []any:
			n := 0
			for _, r := range key {
				n = n*10 + int(r-'0')
			}
			if n >= len(c) {
				return nil
			}
			cur = c[n]
		default:
			return nil
		}
	}
	return cur
}

// TestHL7JSONSubcomponents: a component with subcomponents is an object of
// them; escapes are decoded after splitting, so \T\ is a literal & and an
// escaped separator never splits.
func TestHL7JSONSubcomponents(t *testing.T) {
	for _, tt := range []struct {
		name, msg string
		want      map[string]any
	}{
		{"standard", "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5.1\rPID|1||123^^^HOSP&1.2.3&ISO~9^^^X||A\\T\\B^J\\S\\K\r", map[string]any{
			"PID.3.4.1": "HOSP", "PID.3.4.2": "1.2.3", "PID.3.4.3": "ISO",
			"PID.3.repetitions.1.4": "X", "PID.5.1": "A&B", "PID.5.2": "J^K",
		}},
		{"empty subcomponents", "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\rPID|1||1^^^&&ISO~^^^&&\r", map[string]any{
			"PID.3.4.1": nil, "PID.3.4.3": "ISO", "PID.3.repetitions.1.4": nil,
		}},
		{"custom delimiters", "MSH#$%!@#A#B#C#D#1##ADT$A01#C1#P#2.4\rPID#1##123$$$H@O!T!X!F!\r", map[string]any{
			"PID.3.4.1": "H", "PID.3.4.2": "O@X#", "MSH.2.4": "@",
		}},
		{"kept sequences", "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.3\rNTE|1||line\\.br\\two \\H\\bold\\N\\\r", map[string]any{
			"NTE.3.1": "line\\.br\\two \\H\\bold\\N\\",
		}},
	} {
		doc, err := HL7JSON([]byte(tt.msg))
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		b, _ := json.Marshal(doc)
		var back map[string]any
		_ = json.Unmarshal(b, &back)
		for path, want := range tt.want {
			if v := lookup(back, path); v != want {
				t.Errorf("%s: %s = %v, want %v", tt.name, path, v, want)
			}
		}
	}
}

// TestHL7JSONVersions: MSH-12 must be 2.1 to 2.9 (with a minor release) or
// absent.
func TestHL7JSONVersions(t *testing.T) {
	for ver, ok := range map[string]bool{
		"2.1": true, "2.3.1": true, "2.5": true, "2.5.1": true, "2.8.2": true, "2.9": true, "": true,
		"2.5^^2.5": true, // VID.1 is the version
		"3.0":      false, "2": false, "2.0": false, "2.10": false, "V2.5": false, "2.5.1.1": false,
	} {
		_, err := HL7JSON([]byte("MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|" + ver + "\r"))
		var r *RefusedError
		switch {
		case ok && err != nil:
			t.Errorf("%q: %v", ver, err)
		case !ok && (!errors.As(err, &r) || !errors.Is(err, ErrNotHL7) || r.Reason != "unsupported HL7 version (MSH-12 must be 2.1 to 2.9)"):
			t.Errorf("%q: %v", ver, err)
		}
	}
}

// TestHL7RoundTrip: serializing a parsed message gives it back byte for
// byte, with its own delimiters, escapes, subcomponents, and repetitions.
func TestHL7RoundTrip(t *testing.T) {
	for _, msg := range []string{
		"MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\rPID|1||123^^^HOSP&1.2.3&ISO~9||A\\T\\B^J\\S\\K\\X0D\\\rNTE|1||\\.br\\\r",
		"MSH#$%!@#A#B#C#D#1##ADT$A01#C1#P#2.4\rPID#1##1$$$H@O%2###a!F!b\r",
		"MSH|^~\\&|A\rZZ1|||~~|\r",
	} {
		c := HL7v2()
		v, err := c.Parse([]byte(msg))
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.Serialize(v)
		if err != nil || string(out) != msg {
			t.Errorf("round trip:\n got %q\nwant %q (%v)", out, msg, err)
		}
	}
}

// TestHL7ACKCustomDelimiters: an ACK uses the standard delimiters, and
// values echoed from a message with others are rewritten for them.
func TestHL7ACKCustomDelimiters(t *testing.T) {
	ack, err := HL7ACK([]byte("MSH#$%!@#LAB|X#H$Y@Z#W#H#1##ADT$A01#C!F!1#P#2.4\r"), HL7AckOptions{Code: AckApplicationAccept, ControlID: "A1"})
	if err != nil {
		t.Fatal(err)
	}
	segs := strings.Split(strings.TrimSuffix(string(ack), "\r"), "\r")
	if f := strings.Split(segs[0], "|"); len(f) < 12 || f[4] != "LAB\\F\\X" || f[5] != "H" || f[8] != "ACK^A01" {
		t.Errorf("ACK MSH = %q", segs[0])
	}
	if segs[1] != "MSA|AA|C#1" {
		t.Errorf("ACK MSA = %q", segs[1])
	}
	if code, id, ok := ParseHL7ACK(ack); !ok || code != "AA" || id != "C#1" {
		t.Errorf("ParseHL7ACK = %q %q %v", code, id, ok)
	}
}
