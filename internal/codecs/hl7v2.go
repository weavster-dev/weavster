package codecs

import (
	"bytes"
	"fmt"
	"strings"
)

// HL7Message is the structured form of an HL7 v2 message.
type HL7Message struct {
	Segments []HL7Segment
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
}

// HL7v2 returns an HL7 v2 codec with standard delimiters.
func HL7v2() *HL7Codec {
	return &HL7Codec{fieldSep: '|', compSep: '^', repSep: '~'}
}

func (c *HL7Codec) Name() string { return "hl7v2" }

func (c *HL7Codec) Parse(in []byte) (any, error) {
	text := normalizeSegTerminators(string(in))
	lines := strings.Split(text, "\r")
	msg := &HL7Message{}
	seps := *c
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Resolve delimiters declared by the MSH segment.
		if strings.HasPrefix(line, "MSH") && len(line) > 3 {
			seps.fieldSep = line[3]
			if enc := mshEncoding(line, seps.fieldSep); len(enc) >= 4 {
				seps.compSep = enc[1]
				seps.repSep = enc[2]
			}
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
		comps := strings.Split(rep, string(c.compSep))
		for i := range comps {
			comps[i] = unescapeHL7(comps[i])
		}
		field = append(field, comps)
	}
	return field
}

func (c *HL7Codec) Serialize(v any) ([]byte, error) {
	msg, ok := v.(*HL7Message)
	if !ok {
		return nil, fmt.Errorf("codec: hl7v2: serialize expects *HL7Message, got %T", v)
	}
	var buf bytes.Buffer
	for _, seg := range msg.Segments {
		buf.WriteString(c.serializeSegment(seg))
		buf.WriteByte('\r')
	}
	return buf.Bytes(), nil
}

func (c *HL7Codec) serializeSegment(seg HL7Segment) string {
	var b strings.Builder
	b.WriteString(seg.Name)
	for fi, field := range seg.Fields {
		b.WriteByte(c.fieldSep)
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
				b.WriteByte(c.repSep)
			}
			for ci, comp := range rep {
				if ci > 0 {
					b.WriteByte(c.compSep)
				}
				b.WriteString(escapeHL7(comp))
			}
		}
	}
	return b.String()
}

func (c *HL7Codec) Acknowledge(in []byte) ([]byte, error) {
	v, err := c.Parse(in)
	if err != nil {
		return nil, err
	}
	ack := hl7ACK(v.(*HL7Message))
	return c.Serialize(ack)
}

func normalizeSegTerminators(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\r")
	s = strings.ReplaceAll(s, "\n", "\r")
	return s
}

func unescapeHL7(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	r := strings.NewReplacer(
		`\F\`, "|",
		`\S\`, "^",
		`\R\`, "~",
		`\T\`, "&",
		`\E\`, `\`,
	)
	return r.Replace(s)
}

func escapeHL7(s string) string {
	r := strings.NewReplacer(
		"|", `\F\`,
		"^", `\S\`,
		"~", `\R\`,
		"&", `\T\`,
		`\`, `\E\`,
	)
	return r.Replace(s)
}
