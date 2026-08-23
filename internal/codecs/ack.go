package codecs

import "time"

// Acknowledgment codes (HL7 MSA-1 and X12 AK9-1 equivalents).
const (
	// AckApplicationAccept marks an application-level accept (HL7 "AA").
	AckApplicationAccept = "AA"
	// AckApplicationError marks an application-level error (HL7 "AE").
	AckApplicationError = "AE"
	// AckApplicationReject marks an application-level reject (HL7 "AR").
	AckApplicationReject = "AR"
)

// findSegment returns the first segment with the given name, or nil.
func findSegment(msg *HL7Message, name string) *HL7Segment {
	for i := range msg.Segments {
		if msg.Segments[i].Name == name {
			return &msg.Segments[i]
		}
	}
	return nil
}

// hl7Value returns the first component of field n as a string ("" if absent).
func hl7Value(seg *HL7Segment, n int) string {
	if seg == nil {
		return ""
	}
	f := seg.Field(n)
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// hl7Field wraps a single component into a field value.
func hl7Field(v string) [][]string { return [][]string{{v}} }

// hl7ACK builds a minimal ACK (MSH + MSA) echoing the original control id and
// swapping sender/receiver (spec §7).
func hl7ACK(msg *HL7Message) *HL7Message {
	msh := findSegment(msg, "MSH")
	controlID := hl7Value(msh, 10)
	ackMSH := HL7Segment{Name: "MSH", Fields: [][][]string{
		{{"^", "~", `\`, "&"}},     // MSH-2 encoding characters
		hl7Field(hl7Value(msh, 5)), // sending app = original receiving app
		hl7Field(hl7Value(msh, 6)), // sending facility = original receiving facility
		hl7Field(hl7Value(msh, 3)), // receiving app = original sending app
		hl7Field(hl7Value(msh, 4)), // receiving facility = original sending facility
		hl7Field(hl7Value(msh, 7)), // timestamp
		{{"ACK"}},                  // MSH-9 message type
		hl7Field(controlID),        // MSH-10 message control id
		hl7Field(hl7Value(msh, 11)),
		hl7Field(hl7Value(msh, 12)),
	}}
	msa := HL7Segment{Name: "MSA", Fields: [][][]string{
		{{AckApplicationAccept}},
		hl7Field(controlID),
	}}
	return &HL7Message{Segments: []HL7Segment{ackMSH, msa}}
}

// x12Ack997 builds a minimal 997 functional acknowledgment echoing the
// interchange/group/transaction control numbers (spec §7).
func x12Ack997(doc *EDIDocument) (*EDIDocument, error) {
	var isaCtrl, gsCtrl, stCtrl string
	for _, seg := range doc.Segments {
		switch seg.ID {
		case "ISA":
			isaCtrl = seg.Element(13)
		case "GS":
			gsCtrl = seg.Element(6)
		case "ST":
			stCtrl = seg.Element(2)
		}
	}
	if isaCtrl == "" {
		isaCtrl = "000000001"
	}
	if gsCtrl == "" {
		gsCtrl = "1"
	}
	if stCtrl == "" {
		stCtrl = "0001"
	}
	date := time.Now().Format("060102")
	clock := time.Now().Format("1504")
	ack := &EDIDocument{Segments: []EDISegment{
		{ID: "ISA", Elements: [][]string{
			{"00"}, {" "}, {"00"}, {" "}, {"ZZ"}, {"RECEIVER"}, {"ZZ"}, {"SENDER"},
			{date}, {clock}, {"U"}, {"00401"}, {isaCtrl}, {"0"}, {"P"}, {">"},
		}},
		{ID: "GS", Elements: [][]string{{"FA"}, {"SENDER"}, {"RECEIVER"}, {date}, {clock}, {gsCtrl}, {"X"}, {"004010"}}},
		{ID: "ST", Elements: [][]string{{"997"}, {stCtrl}}},
		{ID: "AK1", Elements: [][]string{{gsCtrl}, {" "}}},
		{ID: "AK9", Elements: [][]string{{"A"}, {"1"}, {"1"}, {"1"}}},
		{ID: "SE", Elements: [][]string{{"6"}, {stCtrl}}},
		{ID: "GE", Elements: [][]string{{"1"}, {gsCtrl}}},
		{ID: "IEA", Elements: [][]string{{"1"}, {isaCtrl}}},
	}}
	return ack, nil
}
