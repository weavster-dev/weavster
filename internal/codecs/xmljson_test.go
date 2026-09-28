package codecs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestXMLJSON(t *testing.T) {
	doc, err := XMLJSON([]byte(`<?xml version="1.0"?>
<!-- an order -->
<order id="42" xmlns="urn:orders" xmlns:x="urn:x">
  <patient><name>DOE &amp; SON</name><x:mrn>123</x:mrn></patient>
  <item sku="A"/>
  <item sku="B">second</item>
</order>`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(doc)
	var back map[string]any
	_ = json.Unmarshal(got, &back)
	for path, want := range map[string]any{
		"order.@id":                "42",
		"order.#ns":                "urn:orders",
		"order.patient.name.#text": "DOE & SON",
		"order.patient.mrn.#text":  "123",
		"order.patient.mrn.#ns":    "urn:x",
		"order.item.@sku":          "A",
		"order.#children.2.@sku":   "B",
		"order.#children.2.#text":  "second",
		"order.#text":              nil,
		"order.@xmlns":             nil,
		"order.@x":                 nil,
	} {
		if v := lookup(back, path); v != want {
			t.Errorf("%s = %v, want %v", path, v, want)
		}
	}
	if n := len(lookup(back, "order.#children").([]any)); n != 3 {
		t.Errorf("order has %d children, want 3", n)
	}

	for name, in := range map[string]string{
		"empty":          "",
		"not xml":        "hello",
		"two roots":      "<a/><b/>",
		"unclosed":       "<a><b></a>",
		"text outside":   "<a/>trailing",
		"unknown entity": `<!DOCTYPE a [<!ENTITY e SYSTEM "file:///etc/passwd">]><a>&e;</a>`,
		"too deep":       strings.Repeat("<a>", MaxXMLDepth+1) + strings.Repeat("</a>", MaxXMLDepth+1),
	} {
		if _, err := XMLJSON([]byte(in)); !errors.Is(err, ErrNotXML) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A DOCTYPE is allowed and ignored; its entities are never expanded.
	if doc, err := XMLJSON([]byte(`<!DOCTYPE a [<!ENTITY e "boom">]><a>x</a>`)); err != nil || lookup(doc, "a.#text") != "x" {
		t.Errorf("DOCTYPE: %v, %v", doc, err)
	}
	if _, err := XMLJSON([]byte(strings.Repeat("<a>", MaxXMLDepth) + strings.Repeat("</a>", MaxXMLDepth))); err != nil {
		t.Errorf("at the depth limit: %v", err)
	}
}
