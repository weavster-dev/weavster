package codecs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// view parses in and returns its JSON round trip (what the DSL sees).
func view(t *testing.T, in string) map[string]any {
	t.Helper()
	doc, err := XMLJSON([]byte(in))
	if err != nil {
		t.Fatalf("%q: %v", in, err)
	}
	b, _ := json.Marshal(doc)
	var back map[string]any
	_ = json.Unmarshal(b, &back)
	return back
}

func TestXMLJSON(t *testing.T) {
	back := view(t, `<?xml version="1.0"?>
<!-- an order -->
<order id="42" xmlns="urn:orders" xmlns:x="urn:x" x:id="7" xml:lang="en">
  <patient>
    <name>
      DOE &amp; SON
    </name>
    <x:mrn>123</x:mrn>
  </patient>
  <item sku="A"/>
  <item sku="B">second</item>
</order>`)
	for path, want := range map[string]any{
		"order.@id":                "42",
		"order.@x:id":              "7",
		"order.@xml:lang":          "en",
		"order.#ns":                "urn:orders",
		"order.patient.name.#text": "DOE & SON",
		"order.patient.mrn.#text":  "123",
		"order.patient.mrn.#ns":    "urn:x",
		"order.item.0.@sku":        "A",
		"order.item.1.@sku":        "B",
		"order.item.1.#text":       "second",
		"order.#text":              nil,
		"order.@xmlns":             nil,
		"order.@x":                 nil,
		"order.#children":          nil,
	} {
		if v := lookup(back, path); v != want {
			t.Errorf("%s = %v, want %v", path, v, want)
		}
	}

	// Every element appears once: deep nesting stays small.
	deep := strings.Repeat("<a><b/>", 200) + strings.Repeat("</a>", 200)
	doc, err := XMLJSON([]byte(deep))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := json.Marshal(doc); len(b) > 10*len(deep) {
		t.Errorf("a %d-byte document became %d bytes of JSON", len(deep), len(b))
	}

	// Other encodings and a byte order mark.
	if v := lookup(view(t, "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a>caf\xe9</a>"), "a.#text"); v != "café" {
		t.Errorf("ISO-8859-1: %v", v)
	}
	if v := lookup(view(t, "\xEF\xBB\xBF<a>x</a>"), "a.#text"); v != "x" {
		t.Errorf("BOM: %v", v)
	}
	// A DOCTYPE is allowed and ignored; its entities are never expanded.
	if v := lookup(view(t, `<!DOCTYPE a [<!ENTITY e "boom">]><a>x</a>`), "a.#text"); v != "x" {
		t.Errorf("DOCTYPE: %v", v)
	}
	view(t, strings.Repeat("<a>", MaxXMLDepth)+strings.Repeat("</a>", MaxXMLDepth))
	view(t, `<?xml version="1.0"?><?app hint?><a><?app inner?></a><?app after?>`)
	view(t, `<a xmlns:x="urn:x" xmlns:y="urn:y" x:id="1" y:id="2" id="3"/>`)

	for name, tt := range map[string]struct{ in, reason string }{
		"empty":                 {"", "no root element"},
		"not xml":               {"hello", "text outside the root element"},
		"two roots":             {"<a/><b/>", "more than one root element"},
		"unclosed":              {"<a><b></a>", ""},
		"text outside":          {"<a/>trailing", "text outside the root element"},
		"unknown entity":        {`<!DOCTYPE a [<!ENTITY e SYSTEM "file:///etc/passwd">]><a>&e;</a>`, ""},
		"unknown charset":       {`<?xml version="1.0" encoding="x-nope"?><a/>`, ""},
		"undeclared":            {"<a><p:b/></a>", "undeclared namespace prefix"},
		"undeclared attr":       {`<a p:x="1"/>`, "undeclared namespace prefix"},
		"too deep":              {strings.Repeat("<a>", MaxXMLDepth+1) + strings.Repeat("</a>", MaxXMLDepth+1), "elements nested deeper than 256"},
		"too many":              {"<r>" + strings.Repeat("<a/>", MaxXMLElements) + "</r>", "more than 100000 elements"},
		"end tag mismatch":      {"<a></b>", ""},
		"prefix mismatch":       {`<x:a xmlns:x="urn:x" xmlns:y="urn:x"></y:a>`, ""},
		"not closed":            {"<a>", ""},
		"stray end":             {"</a>", ""},
		"bad directive":         {"<!bad><a/>", ""},
		"late declaration":      {`<a/><?xml version="1.0"?>`, ""},
		"declaration in root":   {`<a><?xml version="1.0"?></a>`, ""},
		"doctype after root":    {`<a/><!DOCTYPE a>`, ""},
		"two doctypes":          {`<!DOCTYPE a><!DOCTYPE a><a/>`, ""},
		"duplicate attribute":   {`<a id="1" id="2"/>`, "duplicate attribute"},
		"same expanded name":    {`<a xmlns:x="urn:x" xmlns:y="urn:x" x:id="1" y:id="2"/>`, "duplicate attribute"},
		"duplicate declaration": {`<a xmlns:x="urn:x" xmlns:x="urn:y"/>`, "duplicate attribute"},
	} {
		_, err := XMLJSON([]byte(tt.in))
		var e *NotXMLError
		if !errors.As(err, &e) || e.Reason != tt.reason || !errors.Is(err, ErrNotXML) {
			t.Errorf("%s: %v, want reason %q", name, err, tt.reason)
		}
	}
	if (&NotXMLError{}).Error() != "not a well-formed XML document" || notXML("x").Error() != "not a well-formed XML document: x" {
		t.Error("NotXMLError text")
	}
}
