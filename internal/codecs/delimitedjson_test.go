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
	if b, _ := json.Marshal(mustDelimited(t, "", false)); string(b) != `{"rows":[]}` {
		t.Errorf("empty: %s", b)
	}

	for name, tt := range map[string]struct {
		in     string
		header bool
		reason string
	}{
		"ragged":           {"a,b\n1\n", true, "rows have different numbers of fields"},
		"ragged no header": {"1,2\n3\n", false, "rows have different numbers of fields"},
		"bad quote":        {"a\n\"x\"y\n", true, ""},
		"duplicate name":   {"a,a\n1,2\n", true, "header names must be unique and not empty"},
		"empty name":       {"a,\n1,2\n", true, "header names must be unique and not empty"},
		"no header":        {"", true, "no header row"},
		"too many":         {"a\n" + strings.Repeat("1\n", MaxDelimitedRows+1), true, "more than 100000 rows"},
	} {
		_, err := DelimitedJSON([]byte(tt.in), ',', tt.header)
		var e *NotDelimitedError
		if !errors.As(err, &e) || e.Reason != tt.reason || !errors.Is(err, ErrNotDelimited) {
			t.Errorf("%s: %v, want reason %q", name, err, tt.reason)
		}
	}
	if (&NotDelimitedError{}).Error() != "not valid delimited text" {
		t.Error("NotDelimitedError text")
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
