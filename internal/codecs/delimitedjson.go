package codecs

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Limits of DelimitedJSON: rows, and values in all (bounds memory for a
// body of nothing but delimiters).
const (
	MaxDelimitedRows   = 100_000
	MaxDelimitedValues = 1_000_000
)

// ErrNotDelimited reports input that is not valid delimited text.
var ErrNotDelimited = errors.New("not valid delimited text")

func notDelimited(reason string) error { return &RefusedError{Err: ErrNotDelimited, Reason: reason} }

// DelimitedJSON parses delimited text (RFC 4180 quoting) into the JSON view
// the DSL reads (#107 D-63): {"rows": [...]} where, with a header row, each
// row is an object of column name → value and "header" lists the names in
// order (surrounding spaces trimmed; names must be unique, not empty, and
// without dots, so every column has a DSL path); without one, each row is
// a list of values. Every row must have as many fields as the first; blank
// lines are skipped and a UTF-8 byte order mark is ignored. Input with no
// rows at all is refused.
func DelimitedJSON(in []byte, delim rune, header bool) (map[string]any, error) {
	in = bytes.TrimPrefix(in, []byte("\xEF\xBB\xBF"))
	// Checked before parsing allocates the values.
	if countValues(in, delim) > MaxDelimitedValues {
		return nil, notDelimited(fmt.Sprintf("more than %d values", MaxDelimitedValues))
	}
	r := csv.NewReader(bytes.NewReader(in))
	r.Comma = delim
	var names []string
	rows := []any{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		switch {
		case errors.Is(err, csv.ErrFieldCount):
			return nil, notDelimited("rows have different numbers of fields")
		case errors.Is(err, csv.ErrBareQuote):
			return nil, notDelimited(`a " inside a value that is not in quotes`)
		case errors.Is(err, csv.ErrQuote):
			return nil, notDelimited(`a quoted value is not closed, or has a " not doubled`)
		case err != nil:
			return nil, notDelimited("") // not the reader's text: it can quote the input
		}
		if header && names == nil {
			seen := map[string]bool{}
			for i, n := range rec {
				n = strings.TrimSpace(n)
				if n == "" || seen[n] || strings.Contains(n, ".") {
					return nil, notDelimited("header names must be unique, not empty, and without dots")
				}
				seen[n], rec[i] = true, n
			}
			names = rec
			continue
		}
		if len(rows) == MaxDelimitedRows {
			return nil, notDelimited(fmt.Sprintf("more than %d rows", MaxDelimitedRows))
		}
		if !header {
			vals := make([]any, len(rec))
			for i, v := range rec {
				vals[i] = v
			}
			rows = append(rows, vals)
			continue
		}
		row := make(map[string]any, len(rec))
		for i, v := range rec {
			row[names[i]] = v
		}
		rows = append(rows, row)
	}
	if names == nil && len(rows) == 0 {
		return nil, notDelimited("no rows")
	}
	doc := map[string]any{"rows": rows}
	if header {
		h := make([]any, len(names))
		for i, n := range names {
			h[i] = n
		}
		doc["header"] = h
	}
	return doc, nil
}

// countValues counts the values in delimited text as the parser will see
// them: delimiters and line breaks inside double quotes are part of a value,
// and blank lines hold none.
func countValues(in []byte, delim rune) int {
	values, quoted, rowStarted := 0, false, false
	for _, r := range string(in) {
		switch {
		case r == '"':
			quoted = !quoted // a doubled quote toggles twice
		case quoted:
		case r == delim:
			values++
		case r == '\n':
			if rowStarted {
				values++ // the row's last value
			}
			rowStarted = false
			continue
		case r == '\r':
			continue
		}
		rowStarted = true
	}
	if rowStarted {
		values++
	}
	return values
}
