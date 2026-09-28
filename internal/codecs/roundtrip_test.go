package codecs

import (
	"strings"
	"testing"
)

// TestCodecRoundTrip: parsing then serializing gives the input back for
// input already in each codec's canonical form, and the stated
// equivalent otherwise (#107 D-74).
func TestCodecRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		name, codec, in, want string // want "" means the input
	}{
		{"xml namespaces and mixed content", "xml", `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
			`<!-- orders --><o:order xmlns:o="urn:orders" xmlns="urn:default" id="1" o:v="2"><o:item n="a &amp; b">x &lt; y</o:item>` +
			`Some <b>bold</b> text<?pi data?><empty/></o:order>` + "\n", ""},
		{"xml DOCTYPE kept, not expanded", "xml", `<!DOCTYPE note SYSTEM "http://example.com/note.dtd"><note>hi</note>`, ""},
		{"xml formatting normalized", "xml", `<a x='1'><![CDATA[1 < 2]]><b></b></a>`, `<a x="1">1 &lt; 2<b/></a>`},
		{"xml attribute white space", "xml", "<a v=\"tab&#x9;nl&#xA;\"/>", ""},
		{"xml other encoding", "xml", "<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a>caf\xe9</a>", `<?xml version="1.0" encoding="UTF-8"?><a>café</a>`},
		{"json exact numbers", "json", `{"a":[1,2.50,12345678901234567890123],"b":"<&>","c":null,"d":true}`, ""},
		{"json keys sorted", "json", `{ "z": 1, "a": {"y": 2, "b": 3} }`, `{"a":{"b":3,"y":2},"z":1}`},
		{"delimited quoting", "delimited", "id|name|note\n1|\"Doe|John\"|\"say \"\"hi\"\"\"\n2|Ann|\"two\nlines\"", ""},
		{"delimited blank lines and BOM", "delimited", "\xEF\xBB\xBFa|b\n\nc|d\n", "a|b\nc|d"},
		{"raw binary", "raw", "\x00\x01\xff\r\n\x0b", ""},
		{"hl7v2", "hl7v2", "MSH#$%!@#A#B#C#D#1##ADT$A01#M!F!2#P#2.4\rPID#1##1$$$H@O!T!X\r", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Standard().Get(tt.codec)
			if err != nil {
				t.Fatal(err)
			}
			v, err := c.Parse([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			out, err := c.Serialize(v)
			want := tt.want
			if want == "" {
				want = tt.in
			}
			if err != nil || string(out) != want {
				t.Errorf("round trip:\n got %q\nwant %q (%v)", out, want, err)
			}
		})
	}
}

// TestXMLCodecRefuses: malformed documents, a second root, text outside
// the root, and DTD entities are refused; nothing is fetched.
func TestXMLCodecRefuses(t *testing.T) {
	for _, in := range []string{
		"", "<a>", "<a></b>", "<a/><b/>", "text<a/>", "<a/>tail",
		`<!DOCTYPE a [<!ENTITY e SYSTEM "file:///etc/passwd">]><a>&e;</a>`,
		`<a>&undefined;</a>`,
		strings.Repeat("<a>", MaxXMLDepth+1) + strings.Repeat("</a>", MaxXMLDepth+1),
		"<r>" + strings.Repeat("<a/>", MaxXMLElements) + "</r>",
		`<?xml version="1.0" encoding="x-unknown"?><a/>`,
		`<a x="1" x="2"/>`, `<p:a/>`, `<a b:c="1"/>`, `<a/><!DOCTYPE b>`, `<a/><?xml version="1.0"?>`, `<!ENTITY x "y"><a/>`,
		`<!DOCTYPEfoo><a/>`, `<!DOCTYPE ><a/>`, `<p:a xmlns:p=""/>`,
	} {
		if _, err := XML().Parse([]byte(in)); err == nil {
			t.Errorf("%.60q: parsed", in)
		}
	}
	if _, err := XML().Serialize(&XMLDocument{}); err == nil {
		t.Error("a document without a root serialized")
	}
}

// TestJSONCodecRefuses: data after the value is refused.
func TestJSONCodecRefuses(t *testing.T) {
	for _, in := range []string{`{"a":1} {"b":2}`, `{"a":1}x`, ``, `{`} {
		if _, err := JSON().Parse([]byte(in)); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
}

// TestDelimitedCodecRefuses: the codec reads as the server does (quotes,
// rows of equal length), and an unusable delimiter fails instead of
// dropping rows.
func TestDelimitedCodecRefuses(t *testing.T) {
	for _, in := range []string{"a|\"b\nc", "a|5\" pipe|c", "a|b\nc", "a|b\n   \nc|d"} {
		if _, err := NewDelimited('|', true).Parse([]byte(in)); err == nil {
			t.Errorf("%q: parsed", in)
		}
	}
	rows := "h\n" + strings.Repeat("x\n", MaxDelimitedRows)
	if _, err := NewDelimited('|', true).Parse([]byte(rows)); err != nil {
		t.Errorf("%d rows after a header: %v", MaxDelimitedRows, err)
	}
	if _, err := NewDelimited('|', true).Parse([]byte(rows + "x\n")); err == nil {
		t.Errorf("more than %d rows parsed", MaxDelimitedRows)
	}
	for _, delim := range []byte{0, '"', '\n', '\r', 0xA6} {
		c := NewDelimited(delim, false)
		if _, err := c.Parse([]byte("a")); err == nil {
			t.Errorf("delimiter %#x: parsed", delim)
		}
		if _, err := c.Serialize(&Delimited{Rows: [][]string{{"a", "b"}}}); err == nil {
			t.Errorf("delimiter %#x: serialized", delim)
		}
	}
}
