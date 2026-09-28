package codecs

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

// HL7Message is the structured form of an HL7 v2 message. Component
// values are kept as written, escape sequences and subcomponent separators
// included, so serializing a parsed message gives it back unchanged (with
// CR segment terminators); Delimiters.Decode reads a value.
type HL7Message struct {
	Segments []HL7Segment
	// Delimiters are the message's own (from its first MSH segment); the
	// zero value means the standard |^~\&.
	Delimiters HL7Delimiters
}

// HL7Delimiters are an HL7 v2 message's separators and escape character.
// Escape and Subcomponent are 0 when MSH-2 does not declare them.
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
// subcomponents, still as written (one when none is declared).
func (d HL7Delimiters) Subcomponents(v string) []string {
	d = d.orStandard()
	if d.Subcomponent == 0 {
		return []string{v}
	}
	return strings.Split(v, string(d.Subcomponent))
}

// Decode reads a value as written (a subcomponent, or a component without
// subcomponents): \F\ \S\ \R\ \T\ \E\, written with the message's escape
// character, become its own delimiters; other escape sequences (\X…\,
// \H\, \.br\, …) are kept as written.
func (d HL7Delimiters) Decode(v string) string {
	d = d.orStandard()
	if d.Escape == 0 || strings.IndexByte(v, d.Escape) < 0 {
		return v
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		if v[i] == d.Escape && i+2 < len(v) && v[i+2] == d.Escape {
			if r := d.escaped(v[i+1]); r != 0 {
				b.WriteByte(r)
				i += 2
				continue
			}
		}
		b.WriteByte(v[i])
	}
	return b.String()
}

// escaped is the delimiter escape sequence code c stands for (0 for none).
func (d HL7Delimiters) escaped(c byte) byte {
	switch c {
	case 'F':
		return d.Field
	case 'S':
		return d.Component
	case 'R':
		return d.Repetition
	case 'T':
		return d.Subcomponent
	case 'E':
		return d.Escape
	}
	return 0
}

// Encode writes a plain value for a message with delimiters d: each
// delimiter and the escape character become an escape sequence.
func (d HL7Delimiters) Encode(v string) string {
	d = d.orStandard()
	if d.Escape == 0 {
		return v // nothing to escape with
	}
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		code := byte(0)
		switch c := v[i]; {
		case c == d.Escape:
			code = 'E'
		case c == d.Field:
			code = 'F'
		case c == d.Component:
			code = 'S'
		case c == d.Repetition:
			code = 'R'
		case c == d.Subcomponent && c != 0:
			code = 'T'
		}
		if code == 0 {
			b.WriteByte(v[i])
			continue
		}
		b.WriteByte(d.Escape)
		b.WriteByte(code)
		b.WriteByte(d.Escape)
	}
	return b.String()
}

// HL7Segment is a single segment: fields are indexed field -> repetition ->
// component (a field value may repeat, and each repetition may have components).
type HL7Segment struct {
	Name   string
	Fields [][][]string
	// Delimiters are those the segment was written with (the MSH before
	// it; the standard ones before any MSH); zero: the message's.
	Delimiters HL7Delimiters
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
	d HL7Delimiters
}

// HL7v2 returns an HL7 v2 codec with standard delimiters.
func HL7v2() *HL7Codec {
	return &HL7Codec{d: StandardHL7}
}

func (c *HL7Codec) Name() string { return "hl7v2" }

// Parse splits in into segments; each MSH (and batch header FHS or BHS)
// declares the delimiters of the segments from it on (field 1, and field 2:
// component, repetition, escape, subcomponent — an escape or subcomponent
// it leaves out is not used).
func (c *HL7Codec) Parse(in []byte) (any, error) {
	text := normalizeSegTerminators(string(in))
	lines := strings.Split(text, "\r")
	msg := &HL7Message{}
	d := c.d
	seen := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if isHeader(line) && len(line) > 3 {
			d.Field = line[3]
			enc := mshEncoding(line, d.Field)
			if len(enc) >= 2 {
				d.Component, d.Repetition = enc[0], enc[1]
			}
			d.Escape, d.Subcomponent = 0, 0
			if len(enc) >= 3 {
				d.Escape = enc[2]
			}
			if len(enc) >= 4 {
				d.Subcomponent = enc[3]
			}
			if !seen && strings.HasPrefix(line, "MSH") {
				seen = true
				msg.Delimiters = d
			}
		}
		msg.Segments = append(msg.Segments, parseSegment(line, d))
	}
	return msg, nil
}

// isHeader reports whether line is a segment that declares delimiters:
// MSH, or the batch headers FHS and BHS.
func isHeader(line string) bool {
	return strings.HasPrefix(line, "MSH") || strings.HasPrefix(line, "FHS") || strings.HasPrefix(line, "BHS")
}

// mshEncoding returns the raw MSH-2 field (encoding characters) for a line.
func mshEncoding(line string, fieldSep byte) string {
	rest := line[4:]
	if i := strings.IndexByte(rest, fieldSep); i >= 0 {
		return rest[:i]
	}
	return rest
}

func parseSegment(line string, d HL7Delimiters) HL7Segment {
	name, rest, _ := strings.Cut(line, string(d.Field))
	seg := HL7Segment{Name: name, Delimiters: d}
	if isHeader(name) && len(name) == 3 {
		// Field 2 carries the encoding characters and must not be split.
		enc, tail, _ := strings.Cut(rest, string(d.Field))
		comps := make([]string, 0, len(enc))
		for i := 0; i < len(enc); i++ {
			comps = append(comps, string(enc[i]))
		}
		seg.Fields = append(seg.Fields, [][]string{comps})
		rest = tail
	}
	for {
		f, tail, ok := strings.Cut(rest, string(d.Field))
		seg.Fields = append(seg.Fields, parseField(f, d))
		if !ok {
			break
		}
		rest = tail
	}
	return seg
}

func parseField(f string, d HL7Delimiters) [][]string {
	reps := strings.Split(f, string(d.Repetition))
	field := make([][]string, 0, len(reps))
	for _, rep := range reps {
		field = append(field, strings.Split(rep, string(d.Component)))
	}
	return field
}

// Serialize writes each segment with its own delimiters (a segment without
// them: the message's), values as they are (already escaped for them).
func (c *HL7Codec) Serialize(v any) ([]byte, error) {
	msg, ok := v.(*HL7Message)
	if !ok {
		return nil, fmt.Errorf("codec: hl7v2: serialize expects *HL7Message, got %T", v)
	}
	var buf bytes.Buffer
	for _, seg := range msg.Segments {
		buf.WriteString(serializeSegment(seg, seg.delimiters(msg)))
		buf.WriteByte('\r')
	}
	return buf.Bytes(), nil
}

// delimiters are the ones seg is written with.
func (s HL7Segment) delimiters(msg *HL7Message) HL7Delimiters {
	if s.Delimiters.Field != 0 {
		return s.Delimiters
	}
	return msg.Delimiters.orStandard()
}

// serializeSegment writes seg with delimiters d, values as they are
// (already escaped for d).
func serializeSegment(seg HL7Segment, d HL7Delimiters) string {
	var b strings.Builder
	b.WriteString(seg.Name)
	for fi, field := range seg.Fields {
		b.WriteByte(d.Field)
		if isHeader(seg.Name) && len(seg.Name) == 3 && fi == 0 {
			// The encoding characters are joined without separators.
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
