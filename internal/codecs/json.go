package codecs

import (
	"encoding/json"
	"fmt"
)

// JSONCodec parses/serializes JSON using the standard library.
type JSONCodec struct{}

// JSON returns a JSON codec.
func JSON() *JSONCodec { return &JSONCodec{} }

func (c *JSONCodec) Name() string { return "json" }

func (c *JSONCodec) Parse(in []byte) (any, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, fmt.Errorf("codec: json: %w", err)
	}
	return v, nil
}

func (c *JSONCodec) Serialize(v any) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("codec: json: cannot serialize nil")
	}
	return json.Marshal(v)
}

func (c *JSONCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
