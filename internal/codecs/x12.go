package codecs

import (
	"bytes"
	"fmt"
	"strings"
)

// EDIDocument is the structured form of a segment-based EDI payload (X12,
// NCPDP).
type EDIDocument struct {
	Segments []EDISegment
}

// EDISegment is one segment: elements are indexed element -> component.
type EDISegment struct {
	ID       string
	Elements [][]string
}

// Element returns the components of the 1-based element number n ("" if absent).
func (s EDISegment) Element(n int) string {
	if n < 1 || n > len(s.Elements) {
		return ""
	}
	if len(s.Elements[n-1]) == 0 {
		return ""
	}
	return s.Elements[n-1][0]
}

// EDICodec parses/serializes segment-based EDI payloads with configurable
// delimiters (used for both X12 and NCPDP).
type EDICodec struct {
	name                      string
	segTerm, elemSep, compSep byte
	ack                       func(*EDIDocument) (*EDIDocument, error)
}

func newEDI(name string, segTerm, elemSep, compSep byte, ack func(*EDIDocument) (*EDIDocument, error)) *EDICodec {
	return &EDICodec{name: name, segTerm: segTerm, elemSep: elemSep, compSep: compSep, ack: ack}
}

func (c *EDICodec) Name() string { return c.name }

func (c *EDICodec) Parse(in []byte) (any, error) {
	text := strings.TrimSpace(string(in))
	doc := &EDIDocument{}
	if text == "" {
		return doc, nil
	}
	for _, line := range strings.Split(text, string(c.segTerm)) {
		line = strings.Trim(line, "\r\n ")
		if line == "" {
			continue
		}
		doc.Segments = append(doc.Segments, c.parseSegment(line))
	}
	return doc, nil
}

func (c *EDICodec) parseSegment(line string) EDISegment {
	id, rest, _ := strings.Cut(line, string(c.elemSep))
	seg := EDISegment{ID: id}
	for {
		e, tail, ok := strings.Cut(rest, string(c.elemSep))
		seg.Elements = append(seg.Elements, strings.Split(e, string(c.compSep)))
		if !ok {
			break
		}
		rest = tail
	}
	return seg
}

func (c *EDICodec) Serialize(v any) ([]byte, error) {
	doc, ok := v.(*EDIDocument)
	if !ok {
		return nil, fmt.Errorf("codec: %s: serialize expects *EDIDocument, got %T", c.name, v)
	}
	var buf bytes.Buffer
	for _, seg := range doc.Segments {
		buf.WriteString(seg.ID)
		for _, e := range seg.Elements {
			buf.WriteByte(c.elemSep)
			buf.WriteString(strings.Join(e, string(c.compSep)))
		}
		buf.WriteByte(c.segTerm)
	}
	return buf.Bytes(), nil
}

func (c *EDICodec) Acknowledge(in []byte) ([]byte, error) {
	if c.ack == nil {
		return nil, ErrNotSupported
	}
	v, err := c.Parse(in)
	if err != nil {
		return nil, err
	}
	ack, err := c.ack(v.(*EDIDocument))
	if err != nil {
		return nil, err
	}
	return c.Serialize(ack)
}

// X12 returns the standard X12 EDI codec (segment '~', element '*',
// component ':').
func X12() *EDICodec {
	return newEDI("x12", '~', '*', ':', x12Ack997)
}
