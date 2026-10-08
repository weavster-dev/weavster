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

// TestUnescapeHL7AllSequences covers Decode's escape sequences, and Encode
// writing each value back.
func TestUnescapeHL7AllSequences(t *testing.T) {
	cases := map[string]string{
		`no backslash here`: `no backslash here`,
		`a\F\b`:             `a|b`,
		`a\S\b`:             `a^b`,
		`a\R\b`:             `a~b`,
		`a\T\b`:             `a&b`,
		`a\E\b`:             `a\b`,
		`\F\\S\\R\\T\\E\`:   `|^~&\`,
		`a\H\b\N\`:          `a\H\b\N\`, // other sequences are kept
	}
	for in, want := range cases {
		if got := StandardHL7.Decode(in); got != want {
			t.Errorf("Decode(%q) = %q, want %q", in, got, want)
		}
		if got := StandardHL7.Decode(StandardHL7.Encode(want)); got != want {
			t.Errorf("Decode(Encode(%q)) = %q", want, got)
		}
	}
}
