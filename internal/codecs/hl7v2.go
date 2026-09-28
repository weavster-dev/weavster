package codecs

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// HL7Message is the structured form of an HL7 v2 message. Component
// values are kept as written, escape sequences and subcomponent separators
// included, so serializing a parsed message gives it back unchanged;
// Delimiters.Decode reads a value.
type HL7Message struct {
	Segments []HL7Segment
	// Delimiters are the message's own (from its MSH segment); the zero
	// value means the standard |^~\&.
	Delimiters HL7Delimiters
}

// HL7Delimiters are an HL7 v2 message's separators and escape character.
type HL7Delimiters struct {
	Field, Component, Repetition, Escape, Subcomponent byte
}

// StandardHL7 are the standard delimiters |^~\&.
var StandardHL7 = HL7Delimiters{Field: '|', Component: '^', Repetition: '~', Escape: '\\', Subcomponent: '&'}

// orStandard is d, or the standard delimiters for the zero value.
func (d HL7Delimiters) orStandard() HL7Delimiters {
	if d.Field == 0 {
		return StandardHL7
	}
	return d
}

// Subcomponents splits a component value as written into its
// subcomponents, still as written.
func (d HL7Delimiters) Subcomponents(v string) []string {
	return strings.Split(v, string(d.orStandard().Subcomponent))
}

// Decode reads a value as written (a subcomponent, or a component without
// subcomponents): \F\ \S\ \R\ \T\ \E\, written with the message's escape
// character, become its own delimiters; other escape sequences (\X…\,
// \H\, \.br\, …) are kept as written.
func (d HL7Delimiters) Decode(v string) string {
	d = d.orStandard()
	e := string(d.Escape)
	if !strings.Contains(v, e) {
		return v
	}
	return strings.NewReplacer(
		e+"F"+e, string(d.Field),
		e+"S"+e, string(d.Component),
		e+"R"+e, string(d.Repetition),
		e+"T"+e, string(d.Subcomponent),
		e+"E"+e, e,
	).Replace(v)
}

// Encode writes a plain value for a message with delimiters d: each
// delimiter and the escape character become an escape sequence.
func (d HL7Delimiters) Encode(v string) string {
	d = d.orStandard()
	e := string(d.Escape)
	return strings.NewReplacer(
		e, e+"E"+e,
		string(d.Field), e+"F"+e,
		string(d.Component), e+"S"+e,
		string(d.Repetition), e+"R"+e,
		string(d.Subcomponent), e+"T"+e,
	).Replace(v)
}

// HL7Segment is a single segment: fields are indexed field -> repetition ->
// component (a field value may repeat, and each repetition may have components).
type HL7Segment struct {
	Name   string
	Fields [][][]string
}

// Field returns the components of the first repetition of 1-based HL7 field
// number n (field 1 is the segment id, field 2 the first stored field).
func (s HL7Segment) Field(n int) []string {
	idx := n - 2
	if idx < 0 || idx >= len(s.Fields) {
		return nil
	}
	if len(s.Fields[idx]) == 0 {
		return nil
	}
	return s.Fields[idx][0]
}

// HL7Codec parses/serializes HL7 v2 messages. Delimiters are resolved from the
// MSH segment (MSH-1/2) and default to the standard |^~\&.
type HL7Codec struct {
	fieldSep byte
	compSep  byte
	repSep   byte
	escape   byte
	subSep   byte
}

// HL7v2 returns an HL7 v2 codec with standard delimiters.
func HL7v2() *HL7Codec {
	return &HL7Codec{fieldSep: '|', compSep: '^', repSep: '~', escape: '\\', subSep: '&'}
}

func (c *HL7Codec) Name() string { return "hl7v2" }

func (c *HL7Codec) Parse(in []byte) (any, error) {
	text := normalizeSegTerminators(string(in))
	lines := strings.Split(text, "\r")
	msg := &HL7Message{}
	seps := *c
	seen := false // the first MSH declares the message's delimiters
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Resolve delimiters declared by the MSH segment.
		if strings.HasPrefix(line, "MSH") && len(line) > 3 {
			seps.fieldSep = line[3]
			// MSH-2 is component, repetition, escape, subcomponent.
			enc := mshEncoding(line, seps.fieldSep)
			if len(enc) >= 2 {
				seps.compSep = enc[0]
				seps.repSep = enc[1]
			}
			if len(enc) >= 4 {
				seps.escape = enc[2]
				seps.subSep = enc[3]
			}
		}
		if strings.HasPrefix(line, "MSH") && len(line) > 3 && !seen {
			seen = true
			msg.Delimiters = HL7Delimiters{Field: seps.fieldSep, Component: seps.compSep, Repetition: seps.repSep, Escape: seps.escape, Subcomponent: seps.subSep}
		}
		msg.Segments = append(msg.Segments, seps.parseSegment(line))
	}
	return msg, nil
}

// mshEncoding returns the raw MSH-2 field (encoding characters) for a line.
func mshEncoding(line string, fieldSep byte) string {
	rest := line[4:]
	if i := strings.IndexByte(rest, fieldSep); i >= 0 {
		return rest[:i]
	}
	return rest
}

func (c *HL7Codec) parseSegment(line string) HL7Segment {
	name, rest, _ := strings.Cut(line, string(c.fieldSep))
	seg := HL7Segment{Name: name}
	if name == "MSH" {
		// MSH-2 carries the encoding characters and must not be split.
		enc, tail, _ := strings.Cut(rest, string(c.fieldSep))
		comps := make([]string, 0, len(enc))
		for i := 0; i < len(enc); i++ {
			comps = append(comps, string(enc[i]))
		}
		seg.Fields = append(seg.Fields, [][]string{comps})
		rest = tail
	}
	for {
		f, tail, ok := strings.Cut(rest, string(c.fieldSep))
		seg.Fields = append(seg.Fields, c.parseField(f))
		if !ok {
			break
		}
		rest = tail
	}
	return seg
}

func (c *HL7Codec) parseField(f string) [][]string {
	reps := strings.Split(f, string(c.repSep))
	field := make([][]string, 0, len(reps))
	for _, rep := range reps {
		field = append(field, strings.Split(rep, string(c.compSep)))
	}
	return field
}

func (c *HL7Codec) Serialize(v any) ([]byte, error) {
	msg, ok := v.(*HL7Message)
	if !ok {
		return nil, fmt.Errorf("codec: hl7v2: serialize expects *HL7Message, got %T", v)
	}
	d := msg.Delimiters.orStandard()
	var buf bytes.Buffer
	for _, seg := range msg.Segments {
		buf.WriteString(serializeSegment(seg, d))
		buf.WriteByte('\r')
	}
	return buf.Bytes(), nil
}

// serializeSegment writes seg with delimiters d, values as they are
// (already escaped for d).
func serializeSegment(seg HL7Segment, d HL7Delimiters) string {
	var b strings.Builder
	b.WriteString(seg.Name)
	for fi, field := range seg.Fields {
		b.WriteByte(d.Field)
		if seg.Name == "MSH" && fi == 0 {
			// MSH-2 encoding characters are joined without separators.
			for _, rep := range field {
				for _, comp := range rep {
					b.WriteString(comp)
				}
			}
			continue
		}
		for ri, rep := range field {
			if ri > 0 {
				b.WriteByte(d.Repetition)
			}
			for ci, comp := range rep {
				if ci > 0 {
					b.WriteByte(d.Component)
				}
				b.WriteString(comp)
			}
		}
	}
	return b.String()
}

func (c *HL7Codec) Acknowledge(in []byte) ([]byte, error) {
	return HL7ACK(in, HL7AckOptions{Code: AckApplicationAccept, Now: time.Now()})
}

func normalizeSegTerminators(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\r")
	s = strings.ReplaceAll(s, "\n", "\r")
	return s
}
