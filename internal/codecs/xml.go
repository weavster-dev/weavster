package codecs

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// XMLCodec parses and serializes XML into a generic element tree.
//
// XXE safety: encoding/xml does not process DTDs or resolve external
// entities, so external entity injection (XXE) is disabled by construction
// (spec §10).
type XMLCodec struct{}

// XMLNode is a generic element in the parsed tree.
type XMLNode struct {
	Name     xml.Name
	Attrs    []xml.Attr
	Text     string
	Children []*XMLNode
}

// XML returns an XML codec.
func XML() *XMLCodec { return &XMLCodec{} }

func (c *XMLCodec) Name() string { return "xml" }

func (c *XMLCodec) Parse(in []byte) (any, error) {
	dec := xml.NewDecoder(bytes.NewReader(in))
	var root *XMLNode
	var stack []*XMLNode
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("codec: xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &XMLNode{Name: t.Name, Attrs: t.Attr}
			if len(stack) == 0 {
				root = n
			} else {
				parent := stack[len(stack)-1]
				parent.Children = append(parent.Children, n)
			}
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].Text += string(t)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("codec: xml: no root element")
	}
	return root, nil
}

func (c *XMLCodec) Serialize(v any) ([]byte, error) {
	root, ok := v.(*XMLNode)
	if !ok {
		return nil, fmt.Errorf("codec: xml: serialize expects *XMLNode, got %T", v)
	}
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	if err := writeNode(&buf, root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *XMLCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrNotSupported }

func writeNode(buf *bytes.Buffer, n *XMLNode) error {
	buf.WriteByte('<')
	buf.WriteString(n.Name.Local)
	for _, a := range n.Attrs {
		buf.WriteByte(' ')
		buf.WriteString(a.Name.Local)
		buf.WriteString(`="`)
		buf.WriteString(escapeAttr(a.Value))
		buf.WriteByte('"')
	}
	if len(n.Children) == 0 && n.Text == "" {
		buf.WriteString("/>")
		return nil
	}
	buf.WriteByte('>')
	if n.Text != "" {
		if err := xml.EscapeText(buf, []byte(n.Text)); err != nil {
			return err
		}
	}
	for _, ch := range n.Children {
		if err := writeNode(buf, ch); err != nil {
			return err
		}
	}
	buf.WriteString("</")
	buf.WriteString(n.Name.Local)
	buf.WriteByte('>')
	return nil
}

func escapeAttr(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return r.Replace(s)
}
