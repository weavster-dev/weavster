package codecs

import (
	"strings"
	"testing"
)

func TestX12SerializeRejectsWrongType(t *testing.T) {
	_, err := X12().Serialize("not an EDI document")
	if err == nil {
		t.Fatal("Serialize() error = nil, want wrong-type error")
	}
	if want := "codec: x12: serialize expects *EDIDocument, got string"; err.Error() != want {
		t.Fatalf("Serialize() error = %q, want %q", err, want)
	}
}

func TestX12AcknowledgeDefaultsMissingControlIDs(t *testing.T) {
	c := X12()
	ackBytes, err := c.Acknowledge(nil)
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}

	parsed, err := c.Parse(ackBytes)
	if err != nil {
		t.Fatalf("Parse(acknowledgment) error = %v", err)
	}
	doc := parsed.(*EDIDocument)
	segments := make(map[string]EDISegment, len(doc.Segments))
	for _, segment := range doc.Segments {
		segments[segment.ID] = segment
	}

	checks := []struct {
		segment string
		element int
		want    string
	}{
		{segment: "ISA", element: 13, want: "000000001"},
		{segment: "IEA", element: 2, want: "000000001"},
		{segment: "GS", element: 6, want: "1"},
		{segment: "GE", element: 2, want: "1"},
		{segment: "ST", element: 2, want: "0001"},
		{segment: "SE", element: 2, want: "0001"},
	}
	for _, check := range checks {
		t.Run(check.segment, func(t *testing.T) {
			segment, ok := segments[check.segment]
			if !ok {
				t.Fatalf("acknowledgment missing %s segment: %q", check.segment, ackBytes)
			}
			if got := segment.Element(check.element); got != check.want {
				t.Errorf("%s-%d = %q, want %q", check.segment, check.element, got, check.want)
			}
		})
	}

	if ack := string(ackBytes); !strings.Contains(ack, "AK9*A*1*1*1~") {
		t.Errorf("acknowledgment missing acceptance status: %q", ack)
	}
}
