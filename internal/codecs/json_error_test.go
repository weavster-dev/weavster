package codecs

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestJSONParsePreservesSyntaxError(t *testing.T) {
	got, err := JSON().Parse([]byte(`{"message":`))
	if err == nil {
		t.Fatal("Parse() error = nil, want malformed JSON error")
	}
	if got != nil {
		t.Errorf("Parse() value = %#v, want nil", got)
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("Parse() error = %T %v, want wrapped *json.SyntaxError", err, err)
	}
}

func TestJSONSerializePreservesUnsupportedTypeError(t *testing.T) {
	got, err := JSON().Serialize(make(chan int))
	if err == nil {
		t.Fatal("Serialize() error = nil, want unsupported type error")
	}
	if got != nil {
		t.Errorf("Serialize() bytes = %v, want nil", got)
	}
	var typeErr *json.UnsupportedTypeError
	if !errors.As(err, &typeErr) {
		t.Errorf("Serialize() error = %T %v, want *json.UnsupportedTypeError", err, err)
	}
}
