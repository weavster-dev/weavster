package codecs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
)

// XMLCodec parses an XML document into a tree that keeps what the document
// says and in which order, and serializes it back (#107 D-74): namespace
// prefixes and declarations as written, attributes in order, and text,
// child elements, comments, and processing instructions interleaved as
// they came. Formatting inside tags (quote style, entity spelling, empty
// elements written <a></a>, CDATA sections) is not kept; the output is an
// equivalent document in UTF-8.
//
// XXE safety (D-14): encoding/xml processes no DTD and expands no entities
// beyond the predefined ones, and nothing is fetched; a DOCTYPE is kept
// verbatim and never interpreted.
type XMLCodec struct{}

// XMLKind is what an XMLNode is.
type XMLKind int

// XML node kinds.
const (
	XMLElement XMLKind = iota
	XMLText
	XMLComment
	XMLProcInst  // Name.Local is the target, Text the instruction
	XMLDirective // Text is the directive, such as DOCTYPE …
)

// XMLNode is a node of the parsed tree.
type XMLNode struct {
	Kind XMLKind
	// Name and Attrs are an element's, as written: Name.Space and an
	// attribute's Name.Space are prefixes, and namespace declarations are
	// attributes.
	Name  xml.Name
	Attrs []xml.Attr
	// Text is a text, comment, processing instruction, or directive node's
	// content.
	Text string
	// Children are an element's content in document order.
	Children []*XMLNode
}

// XMLDocument is a parsed document: what comes before the root element
// (XML declaration, comments, DOCTYPE, white space), the root, and what
// follows it.
type XMLDocument struct {
	Prolog []*XMLNode
	Root   *XMLNode
	Epilog []*XMLNode
}

// XML returns an XML codec.
func XML() *XMLCodec { return &XMLCodec{} }

func (c *XMLCodec) Name() string { return "xml" }

// Parse reads one well-formed document in any IANA character set (a UTF-8
// byte order mark is skipped), within MaxXMLDepth and MaxXMLElements.
func (c *XMLCodec) Parse(in []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(in, []byte("\xEF\xBB\xBF"))))
	dec.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		enc, err := ianaindex.IANA.Encoding(label)
		if err != nil || enc == nil {
			return nil, fmt.Errorf("unsupported encoding %q", label)
		}
		return enc.NewDecoder().Reader(r), nil
	}
	doc := &XMLDocument{}
	var stack []*XMLNode
	elements := 0
	add := func(n *XMLNode) error {
		switch {
		case len(stack) > 0:
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
		case n.Kind == XMLText && strings.TrimSpace(n.Text) != "":
			return errors.New("codec: xml: text outside the root element")
		case doc.Root == nil:
			doc.Prolog = append(doc.Prolog, n)
		default:
			doc.Epilog = append(doc.Epilog, n)
		}
		return nil
	}
	for {
		// RawToken keeps prefixes as written; end tags are matched here.
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("codec: xml: %w", err)
		}
		var n *XMLNode
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case len(stack) == 0 && doc.Root != nil:
				return nil, errors.New("codec: xml: more than one root element")
			case len(stack) == MaxXMLDepth:
				return nil, fmt.Errorf("codec: xml: elements nested deeper than %d", MaxXMLDepth)
			case elements == MaxXMLElements:
				return nil, fmt.Errorf("codec: xml: more than %d elements", MaxXMLElements)
			}
			elements++
			n = &XMLNode{Kind: XMLElement, Name: t.Name, Attrs: t.Copy().Attr}
			if len(stack) == 0 {
				doc.Root = n
			} else if err := add(n); err != nil {
				return nil, err
			}
			stack = append(stack, n)
			continue
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].Name != t.Name {
				return nil, errors.New("codec: xml: end tag does not match its start tag")
			}
			stack = stack[:len(stack)-1]
			continue
		case xml.CharData:
			n = &XMLNode{Kind: XMLText, Text: string(t)}
		case xml.Comment:
			n = &XMLNode{Kind: XMLComment, Text: string(t)}
		case xml.ProcInst:
			n = &XMLNode{Kind: XMLProcInst, Name: xml.Name{Local: t.Target}, Text: string(t.Inst)}
		case xml.Directive:
			n = &XMLNode{Kind: XMLDirective, Text: string(t)}
		}
		if err := add(n); err != nil {
			return nil, err
		}
	}
	if len(stack) > 0 || doc.Root == nil {
		return nil, errors.New("codec: xml: no complete root element")
	}
	return doc, nil
}

// Serialize writes doc in UTF-8; an XML declaration naming another
// encoding is rewritten to say UTF-8.
func (c *XMLCodec) Serialize(v any) ([]byte, error) {
	doc, ok := v.(*XMLDocument)
	if !ok || doc.Root == nil {
		return nil, fmt.Errorf("codec: xml: serialize expects *XMLDocument with a root, got %T", v)
	}
	var buf bytes.Buffer
	for _, n := range doc.Prolog {
		writeXMLNode(&buf, n)
	}
	writeXMLNode(&buf, doc.Root)
	for _, n := range doc.Epilog {
		writeXMLNode(&buf, n)
	}
	return buf.Bytes(), nil
}

func (c *XMLCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }

// declaredEncoding finds the encoding pseudo-attribute of an XML
// declaration.
var declaredEncoding = regexp.MustCompile(`encoding\s*=\s*("[^"]*"|'[^']*')`)

func writeXMLNode(buf *bytes.Buffer, n *XMLNode) {
	switch n.Kind {
	case XMLText:
		buf.WriteString(escapeXMLText(n.Text))
	case XMLComment:
		buf.WriteString("<!--" + n.Text + "-->")
	case XMLProcInst:
		inst := n.Text
		if strings.EqualFold(n.Name.Local, "xml") {
			inst = declaredEncoding.ReplaceAllString(inst, `encoding="UTF-8"`)
		}
		buf.WriteString("<?" + n.Name.Local)
		if inst != "" {
			buf.WriteString(" " + inst)
		}
		buf.WriteString("?>")
	case XMLDirective:
		buf.WriteString("<!" + n.Text + ">")
	default:
		buf.WriteString("<" + qualified(n.Name))
		for _, a := range n.Attrs {
			buf.WriteString(" " + qualified(a.Name) + `="` + escapeXMLAttr(a.Value) + `"`)
		}
		if len(n.Children) == 0 {
			buf.WriteString("/>")
			return
		}
		buf.WriteString(">")
		for _, ch := range n.Children {
			writeXMLNode(buf, ch)
		}
		buf.WriteString("</" + qualified(n.Name) + ">")
	}
}

// qualified is a name as written: prefix:local, or local.
func qualified(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return n.Space + ":" + n.Local
}

var (
	xmlTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "]]>", "]]&gt;", "\r", "&#xD;")
	xmlAttrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", `"`, "&quot;", "\t", "&#x9;", "\n", "&#xA;", "\r", "&#xD;")
)

func escapeXMLText(s string) string { return xmlTextEscaper.Replace(s) }
func escapeXMLAttr(s string) string { return xmlAttrEscaper.Replace(s) }
