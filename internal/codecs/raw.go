package codecs

import "fmt"

// RawCodec passes through untyped bytes.
type RawCodec struct{}

// Raw returns a raw binary codec.
func Raw() *RawCodec { return &RawCodec{} }

func (c *RawCodec) Name() string { return "raw" }

func (c *RawCodec) Parse(in []byte) (any, error) { return in, nil }

func (c *RawCodec) Serialize(v any) ([]byte, error) {
	switch t := v.(type) {
	case []byte:
		return t, nil
	case string:
		return []byte(t), nil
	default:
		return nil, fmt.Errorf("codec: raw: serialize expects []byte or string, got %T", v)
	}
}

func (c *RawCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }
