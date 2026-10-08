package codecs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestDelimitedJSON(t *testing.T) {
	in := "\xEF\xBB\xBFmrn,lastName,note\r\n123,DOE,\"a, \"\"quoted\"\"\r\nnote\"\r\n\r\n456,ROE,plain\r\n"
	doc, err := DelimitedJSON([]byte(in), ',', true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(doc)
	var back map[string]any
	_ = json.Unmarshal(b, &back)
	for path, want := range map[string]any{
		"rows.0.mrn":      "123",
		"rows.0.lastName": "DOE",
		"rows.0.note":     "a, \"quoted\"\nnote", // CRLF inside quotes becomes LF
		"rows.1.lastName": "ROE",
		"header.1":        "lastName",
		"rows.2":          nil,
	} {
		if v := lookup(back, path); v != want {
			t.Errorf("%s = %q, want %q", path, v, want)
		}
	}

	doc, err = DelimitedJSON([]byte("a|b\nc|d\n"), '|', false)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(doc); string(b) != `{"rows":[["a","b"],["c","d"]]}` {
		t.Errorf("no header: %s", b)
	}
	if b, _ := json.Marshal(mustDelimited(t, "h\n", true)); string(b) != `{"header":["h"],"rows":[]}` {
		t.Errorf("header only: %s", b)
	}
	if b, _ := json.Marshal(mustDelimited(t, " mrn , last name\n1,DOE\n", true)); string(b) != `{"header":["mrn","last name"],"rows":[{"last name":"DOE","mrn":"1"}]}` {
		t.Errorf("header names trimmed: %s", b)
	}

	for name, tt := range map[string]struct {
		in     string
		header bool
		reason string
	}{
		"ragged":           {"a,b\n1\n", true, "rows have different numbers of fields"},
		"ragged no header": {"1,2\n3\n", false, "rows have different numbers of fields"},
		"bad quote":        {"a\n\"x\"y\n", true, `a quoted value is not closed, or has a " not doubled`},
		"bare quote":       {"a\nWidget 5\" pipe\n", true, `a " inside a value that is not in quotes`},
		"unclosed quote":   {"a\n\"x\n", true, `a quoted value is not closed, or has a " not doubled`},
		"duplicate name":   {"a,a\n1,2\n", true, "header names must be unique, not empty, and without dots"},
		"spaced duplicate": {"a, a\n1,2\n", true, "header names must be unique, not empty, and without dots"},
		"empty name":       {"a,\n1,2\n", true, "header names must be unique, not empty, and without dots"},
		"dotted name":      {"patient.mrn\n1\n", true, "header names must be unique, not empty, and without dots"},
		"no header":        {"", true, "no rows"},
		"empty no header":  {"", false, "no rows"},
		"blank lines":      {"\r\n\r\n", false, "no rows"},
		"too many rows":    {"a\n" + strings.Repeat("1\n", MaxDelimitedRows+1), true, "more than 100000 rows"},
		"too many values":  {strings.Repeat(",", MaxDelimitedValues), false, "more than 1000000 values"},
	} {
		_, err := DelimitedJSON([]byte(tt.in), ',', tt.header)
		var e *RefusedError
		if !errors.As(err, &e) || e.Reason != tt.reason || !errors.Is(err, ErrNotDelimited) {
			t.Errorf("%s: %v, want reason %q", name, err, tt.reason)
		}
	}
	// Quoted delimiters and line breaks are not values; the limit itself is allowed.
	for in, want := range map[string]int{
		"":                   0,
		"a,b\r\nc,d\n":       4,
		"\n\n\"x,y\ny\",z\n": 2,
		"a,\"\"\"q\"\"\",c":  3,
		",\n":                2,
	} {
		if got := countValues([]byte(in), ','); got != want {
			t.Errorf("countValues(%q) = %d, want %d", in, got, want)
		}
	}
	if _, err := DelimitedJSON([]byte(strings.Repeat(",", MaxDelimitedValues-1)+"\n"), ',', false); err != nil {
		t.Errorf("exactly %d values: %v", MaxDelimitedValues, err)
	}
	if _, err := DelimitedJSON([]byte("a\n\""+strings.Repeat(",", MaxDelimitedValues)+"\"\n"), ',', true); err != nil {
		t.Errorf("delimiters inside quotes counted: %v", err)
	}
	if notDelimited("").Error() != "not valid delimited text" {
		t.Error("refusal text")
	}
}

func mustDelimited(t *testing.T, in string, header bool) map[string]any {
	t.Helper()
	doc, err := DelimitedJSON([]byte(in), ',', header)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
