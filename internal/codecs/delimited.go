package codecs

import (
	"bytes"
	"fmt"
	"strings"
)

// Delimited is the structured form of a delimited-text payload.
type Delimited struct {
	Header []string
	Rows   [][]string
}

// DelimitedCodec parses/serializes delimited records (tab/pipe/comma).
type DelimitedCodec struct {
	delim     byte
	hasHeader bool
}

// NewDelimited returns a delimited-text codec with the given delimiter and an
// optional leading header row.
func NewDelimited(delim byte, hasHeader bool) *DelimitedCodec {
	return &DelimitedCodec{delim: delim, hasHeader: hasHeader}
}

func (c *DelimitedCodec) Name() string { return "delimited" }

func (c *DelimitedCodec) Parse(in []byte) (any, error) {
	text := strings.TrimRight(string(in), "\r\n")
	d := &Delimited{}
	if text == "" {
		return d, nil
	}
	lines := strings.Split(text, "\n")
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, strings.Split(line, string(c.delim)))
	}
	if c.hasHeader && len(rows) > 0 {
		d.Header = rows[0]
		d.Rows = rows[1:]
	} else {
		d.Rows = rows
	}
	return d, nil
}

func (c *DelimitedCodec) Serialize(v any) ([]byte, error) {
	d, ok := v.(*Delimited)
	if !ok {
		return nil, fmt.Errorf("codec: delimited: serialize expects *Delimited, got %T", v)
	}
	var buf bytes.Buffer
	lines := make([][]string, 0, len(d.Rows)+1)
	if len(d.Header) > 0 {
		lines = append(lines, d.Header)
	}
	lines = append(lines, d.Rows...)
	for i, row := range lines {
		buf.WriteString(strings.Join(row, string(c.delim)))
		if i < len(lines)-1 {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

func (c *DelimitedCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
