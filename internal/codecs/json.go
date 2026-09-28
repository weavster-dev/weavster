package codecs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// JSONCodec parses/serializes JSON using the standard library. A parsed
// value serializes back to the same JSON value (#107 D-74): numbers are
// kept exactly as written (json.Number), strings as they are (no HTML
// escaping); object keys come out sorted and without white space.
type JSONCodec struct{}

// JSON returns a JSON codec.
func JSON() *JSONCodec { return &JSONCodec{} }

func (c *JSONCodec) Name() string { return "json" }

// Parse reads exactly one JSON value.
func (c *JSONCodec) Parse(in []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("codec: json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("codec: json: data after the value")
	}
	return v, nil
}

func (c *JSONCodec) Serialize(v any) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("codec: json: cannot serialize nil")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func (c *JSONCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
