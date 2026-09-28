package codecs

import (
	"bytes"
	"strings"
	"time"
)

// Acknowledgment codes (HL7 MSA-1 and X12 AK9-1 equivalents).
const (
	// AckApplicationAccept marks an application-level accept (HL7 "AA").
	AckApplicationAccept = "AA"
	// AckApplicationError marks an application-level error (HL7 "AE").
	AckApplicationError = "AE"
	// AckApplicationReject marks an application-level reject (HL7 "AR").
	AckApplicationReject = "AR"
	// AckCommitAccept, AckCommitError, and AckCommitReject are the
	// enhanced-mode commit codes ("CA", "CE", "CR").
	AckCommitAccept = "CA"
	AckCommitError  = "CE"
	AckCommitReject = "CR"
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

// HL7AckOptions describe an HL7 acknowledgment: its MSA-1 code (AA, AE,
// or AR), MSA-3 text, the ACK's own MSH-10 control id (default: the
// original's), and its MSH-7 time.
type HL7AckOptions struct {
	Code      string
	Text      string
	ControlID string
	Now       time.Time
}

// HL7ACK builds the MSH + MSA acknowledgment of the HL7 v2 message in: the
// sender and receiver swapped, MSH-9 ACK^<original trigger>, and MSA-2 the
// original control id. A message without an MSH segment gets an ACK with
// those fields empty, so even unreadable input can be answered (AR). A zero
// Now is the current time.
func HL7ACK(in []byte, opts HL7AckOptions) ([]byte, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	c := HL7v2()
	v, err := c.Parse(in)
	if err != nil {
		return nil, err
	}
	return c.Serialize(hl7ACK(v.(*HL7Message), opts))
}

// recode rewrites a component value written with delimiters from for a
// message with delimiters to, subcomponents kept.
func recode(v string, from, to HL7Delimiters) string {
	from, to = from.orStandard(), to.orStandard()
	if from == to {
		return v
	}
	subs := from.Subcomponents(v)
	for i, sub := range subs {
		subs[i] = to.Encode(from.Decode(sub))
	}
	return strings.Join(subs, string(to.Subcomponent))
}

// hl7ACK builds the acknowledgment described by opts (spec §7). It uses the
// standard delimiters; values echoed from a message with other delimiters
// are rewritten for them.
func hl7ACK(msg *HL7Message, opts HL7AckOptions) *HL7Message {
	msh := findSegment(msg, "MSH")
	hl7Value := func(seg *HL7Segment, n int) string {
		return recode(hl7Value(seg, n), msg.Delimiters, StandardHL7)
	}
	controlID := hl7Value(msh, 10)
	ackID := opts.ControlID
	if ackID == "" {
		ackID = controlID
	}
	msgType := []string{"ACK"}
	if msh != nil {
		if typ := msh.Field(9); len(typ) > 1 && typ[1] != "" {
			msgType = append(msgType, recode(typ[1], msg.Delimiters, StandardHL7)) // ACK^<trigger event>
		}
	}
	ackMSH := HL7Segment{Name: "MSH", Fields: [][][]string{
		{{"^", "~", `\`, "&"}},                      // MSH-2 encoding characters
		hl7Field(hl7Value(msh, 5)),                  // sending app = original receiving app
		hl7Field(hl7Value(msh, 6)),                  // sending facility = original receiving facility
		hl7Field(hl7Value(msh, 3)),                  // receiving app = original sending app
		hl7Field(hl7Value(msh, 4)),                  // receiving facility = original sending facility
		hl7Field(opts.Now.Format("20060102150405")), // MSH-7: when the ACK was made
		{{}},            // MSH-8 security
		{msgType},       // MSH-9 message type
		hl7Field(ackID), // MSH-10 message control id
		hl7Field(hl7Value(msh, 11)),
		hl7Field(hl7Value(msh, 12)),
	}}
	msaFields := [][][]string{{{opts.Code}}, hl7Field(controlID)}
	if opts.Text != "" {
		msaFields = append(msaFields, hl7Field(opts.Text))
	}
	msa := HL7Segment{Name: "MSA", Fields: msaFields}
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

// HL7ControlID is MSH-10 of the HL7 v2 message in ("" without one). Only
// the first segment is parsed: MSH comes first.
func HL7ControlID(in []byte) string {
	v, _ := HL7v2().Parse(firstSegmentLine(in)) // never fails
	return hl7Value(findSegment(v.(*HL7Message), "MSH"), 10)
}

// firstSegmentLine is in up to its first segment terminator, line breaks
// before it skipped.
func firstSegmentLine(in []byte) []byte {
	in = bytes.TrimLeft(in, "\r\n")
	if i := bytes.IndexAny(in, "\r\n"); i >= 0 {
		return in[:i]
	}
	return in
}

// ParseHL7ACK reads an acknowledgment: its MSA-1 code (AA, AE, AR, or the
// commit codes CA, CE, CR) and MSA-2, the control id it acknowledges. ok is
// false unless in is an HL7 v2 message (MSH first) with an MSA segment
// carrying a code. Any message type counts: an application may answer with
// a response message (ORR, RSP, …) that carries the MSA.
func ParseHL7ACK(in []byte) (code, controlID string, ok bool) {
	v, _ := HL7v2().Parse(in) // never fails
	segs := v.(*HL7Message).Segments
	if len(segs) == 0 || segs[0].Name != "MSH" {
		return "", "", false
	}
	msa := findSegment(v.(*HL7Message), "MSA")
	// hl7Value numbers fields as MSH does (MSH-1 is the separator), so
	// MSA-1 is its field 2 and MSA-2 its field 3.
	code, controlID = hl7Value(msa, 2), hl7Value(msa, 3)
	if code == "" {
		return "", "", false
	}
	return code, controlID, true
}
