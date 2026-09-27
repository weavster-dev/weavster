package codecs

import (
	"encoding/json"
	"errors"
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
	for _, bad := range []string{"", "PID|1\r", "hello", "MS"} {
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
