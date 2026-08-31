package codecs

import (
	"strings"
	"testing"
)

func TestXMLSerializeRejectsUnsupportedValue(t *testing.T) {
	_, err := XML().Serialize("not an XML node")
	if err == nil {
		t.Fatal("Serialize() error = nil, want unsupported-value error")
	}
	if !strings.Contains(err.Error(), "xml: serialize expects *XMLNode") {
		t.Errorf("Serialize() error = %q, want XMLNode contract", err)
	}
}
