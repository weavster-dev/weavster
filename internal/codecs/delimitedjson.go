package codecs

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

// MaxDelimitedRows is how many rows DelimitedJSON accepts.
const MaxDelimitedRows = 100_000

// ErrNotDelimited reports input that is not valid delimited text.
var ErrNotDelimited = errors.New("not valid delimited text")

// NotDelimitedError is refused delimited text and why, in fixed words that
// never quote the text (the reason reaches events and replies).
type NotDelimitedError struct{ Reason string }

func (e *NotDelimitedError) Error() string {
	if e.Reason == "" {
		return ErrNotDelimited.Error()
	}
	return ErrNotDelimited.Error() + ": " + e.Reason
}

// Is makes errors.Is(err, ErrNotDelimited) hold.
func (e *NotDelimitedError) Is(target error) bool { return target == ErrNotDelimited }

func notDelimited(reason string) error { return &NotDelimitedError{Reason: reason} }

// DelimitedJSON parses delimited text (RFC 4180 quoting) into the JSON view
// the DSL reads (#107 D-63): {"rows": [...]} where, with a header row, each
// row is an object of column name → value and "header" lists the names in
// order; without one, each row is a list of values. Every row must have as
// many fields as the first; blank lines are skipped and a UTF-8 byte order
// mark is ignored.
func DelimitedJSON(in []byte, delim rune, header bool) (map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(in, []byte("\xEF\xBB\xBF"))))
	r.Comma = delim
	var names []string
	rows := []any{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if errors.Is(err, csv.ErrFieldCount) {
			return nil, notDelimited("rows have different numbers of fields")
		}
		if err != nil {
			return nil, notDelimited("") // not the reader's text: it can quote the input
		}
		if header && names == nil {
			seen := map[string]bool{}
			for _, n := range rec {
				if n == "" || seen[n] {
					return nil, notDelimited("header names must be unique and not empty")
				}
				seen[n] = true
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
	if header && names == nil {
		return nil, notDelimited("no header row")
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
