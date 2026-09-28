package codecs

import (
	"bytes"
	"encoding/csv"
	"fmt"
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

// Parse reads records with RFC 4180 quoting ("a, b" and "say ""hi"""
// are one field each; a quoted field may span lines); blank lines are
// skipped and rows may differ in length (#107 D-74).
func (c *DelimitedCodec) Parse(in []byte) (any, error) {
	r := csv.NewReader(bytes.NewReader(in))
	r.Comma = rune(c.delim)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("codec: delimited: %w", err)
	}
	d := &Delimited{}
	if c.hasHeader && len(rows) > 0 {
		d.Header = rows[0]
		d.Rows = rows[1:]
	} else {
		d.Rows = rows
	}
	return d, nil
}

// Serialize writes the header and rows, quoting a field only when it needs
// it (it holds the delimiter, a quote, a line break, or starts with a
// space); rows end with \n, except the last.
func (c *DelimitedCodec) Serialize(v any) ([]byte, error) {
	d, ok := v.(*Delimited)
	if !ok {
		return nil, fmt.Errorf("codec: delimited: serialize expects *Delimited, got %T", v)
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = rune(c.delim)
	if len(d.Header) > 0 {
		_ = w.Write(d.Header) // a bytes.Buffer does not fail
	}
	for _, row := range d.Rows {
		_ = w.Write(row)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("codec: delimited: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (c *DelimitedCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
