package codecs

import (
	"strings"
	"testing"
)

// TestRawSerializeUnsupportedType covers RawCodec.Serialize's error branch
// (raw.go), which was previously only exercised via the []byte/string paths.
func TestRawSerializeUnsupportedType(t *testing.T) {
	c := Raw()
	_, err := c.Serialize(42)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "raw: serialize expects") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestUnescapeHL7AllSequences covers unescapeHL7's escape-replacement branches
// (hl7v2.go), previously only exercised via the no-backslash fast path.
func TestUnescapeHL7AllSequences(t *testing.T) {
	cases := map[string]string{
		`no backslash here`: `no backslash here`,
		`a\F\b`:             `a|b`,
		`a\S\b`:             `a^b`,
		`a\R\b`:             `a~b`,
		`a\T\b`:             `a&b`,
		`a\E\b`:             `a\b`,
		`\F\\S\\R\\T\\E\`:   `|^~&\`,
	}
	for in, want := range cases {
		if got := unescapeHL7(in); got != want {
			t.Errorf("unescapeHL7(%q) = %q, want %q", in, got, want)
		}
	}
}
