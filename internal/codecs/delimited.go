package codecs

import (
	"bytes"
	"encoding/csv"
	"errors"
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

// Parse reads records the way the server reads delimited text
// (readDelimited: RFC 4180 quoting, blank lines skipped, rows of equal
// length, the same limits, MaxDelimitedRows rows after the header; #107
// D-74).
func (c *DelimitedCodec) Parse(in []byte) (any, error) {
	var rows [][]string
	limit := MaxDelimitedRows
	if c.hasHeader {
		limit++
	}
	if err := readDelimited(in, rune(c.delim), func(rec []string) error {
		if len(rows) == limit {
			return notDelimited(fmt.Sprintf("more than %d rows", MaxDelimitedRows))
		}
		rows = append(rows, rec)
		return nil
	}); err != nil {
		return nil, err
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
	if !validDelimiter(rune(c.delim)) {
		return nil, errors.New("codec: delimited: the delimiter must be an ASCII character other than a quote or a line break")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = rune(c.delim)
	rows := d.Rows
	if len(d.Header) > 0 {
		rows = append([][]string{d.Header}, rows...)
	}
	if err := w.WriteAll(rows); err != nil {
		return nil, fmt.Errorf("codec: delimited: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (c *DelimitedCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
