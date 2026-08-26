package codecs

import (
	"strings"
	"testing"
)

// TestEDISegmentElementBoundaries covers the three early-return branches of
// EDISegment.Element (x12.go): n<1, n beyond the element count, and an empty
// element slice. The happy path was already covered via x12Ack997.
func TestEDISegmentElementBoundaries(t *testing.T) {
	seg := EDISegment{ID: "ST", Elements: [][]string{{"270"}, {}}}

	cases := []struct {
		n    int
		want string
	}{
		{0, ""},    // n < 1
		{-1, ""},   // n < 1 (negative)
		{3, ""},    // n > len(Elements)
		{2, ""},    // present but empty element slice
		{1, "270"}, // happy path
	}
	for _, tc := range cases {
		if got := seg.Element(tc.n); got != tc.want {
			t.Errorf("Element(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestHL7SegmentFieldBoundaries covers the three early-return branches of
// HL7Segment.Field (hl7v2.go): field index below the stored fields, index
// beyond the stored fields, and an empty field slice.
func TestHL7SegmentFieldBoundaries(t *testing.T) {
	seg := HL7Segment{Name: "PID", Fields: [][][]string{
		{{"component-0"}}, // field 2
		{},                // field 3 (empty)
	}}

	cases := []struct {
		n    int
		want []string
	}{
		{1, nil}, // idx = -1 (segment id)
		{2, []string{"component-0"}},
		{3, nil}, // empty field slice
		{5, nil}, // beyond stored fields
	}
	for _, tc := range cases {
		got := seg.Field(tc.n)
		if len(got) != len(tc.want) {
			t.Errorf("Field(%d) = %v, want %v", tc.n, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Field(%d)[%d] = %q, want %q", tc.n, i, got[i], tc.want[i])
			}
		}
	}
}

// TestRegisterOnZeroValueRegistry covers the lazy-initialization branch of
// Registry.Register (codec.go), where the internal map is nil.
func TestRegisterOnZeroValueRegistry(t *testing.T) {
	var r Registry // zero value: m == nil
	r.Register(JSON())
	if got, err := r.Get("json"); err != nil || got == nil {
		t.Errorf("Get after Register on zero-value registry = %v, %v", got, err)
	}
}

// TestFormatAmountErrors covers the error branches of FormatAmount (ncpdp.go):
// empty, non-numeric, more than two decimals, negative, and width overflow.
func TestFormatAmountErrors(t *testing.T) {
	cases := []struct {
		amount string
		width  int
		substr string
	}{
		{"", 6, "empty amount"},
		{"abc", 6, "invalid amount"},
		{"1.234", 6, "exceeds two decimals"},
		{"-1.00", 6, "negative amount"},
		{"1234.56", 4, "overflows width"},
	}
	for _, tc := range cases {
		_, err := FormatAmount(tc.amount, tc.width)
		if err == nil {
			t.Errorf("FormatAmount(%q, %d) = nil, want error containing %q", tc.amount, tc.width, tc.substr)
			continue
		}
		if !strings.Contains(err.Error(), tc.substr) {
			t.Errorf("FormatAmount(%q, %d) error = %q, want substring %q", tc.amount, tc.width, err, tc.substr)
		}
	}
}

// TestParseAmountError covers the invalid-numeric-field branch of ParseAmount
// (ncpdp.go).
func TestParseAmountError(t *testing.T) {
	if _, err := ParseAmount("12x4"); err == nil {
		t.Error("ParseAmount with non-numeric field = nil, want error")
	}
}

// TestHL7ValueNilSegment covers hl7Value's nil-segment guard (ack.go), which
// must return "" instead of panicking when the MSH segment is absent.
func TestHL7ValueNilSegment(t *testing.T) {
	if got := hl7Value(nil, 10); got != "" {
		t.Errorf("hl7Value(nil) = %q, want empty", got)
	}
}

// TestFindSegmentMissing covers findSegment's not-found branch (ack.go).
func TestFindSegmentMissing(t *testing.T) {
	msg := &HL7Message{Segments: []HL7Segment{{Name: "PID"}}}
	if got := findSegment(msg, "MSH"); got != nil {
		t.Errorf("findSegment(missing MSH) = %v, want nil", got)
	}
}
