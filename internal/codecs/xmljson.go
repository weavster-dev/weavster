package codecs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxXMLDepth is how deeply XMLJSON lets elements nest.
const MaxXMLDepth = 256

// ErrNotXML reports input that is not a single well-formed XML document.
var ErrNotXML = errors.New("not a well-formed XML document")

// XMLJSON parses an XML document into the JSON view the DSL reads (#107
// D-62): {"<root local name>": element}. An element object has "@<name>"
// for each attribute (namespace declarations left out), "#text" for its own
// text when that is not only whitespace, "#ns" for its namespace URI when it
// has one, each child's local name for the first such child, and
// "#children" for every child element in order.
//
// Safe by construction (D-14): encoding/xml processes no DTD and expands no
// entities beyond the five predefined ones, and nothing is fetched.
func XMLJSON(in []byte) (map[string]any, error) {
	dec := xml.NewDecoder(bytes.NewReader(in))
	var root map[string]any
	var rootName string
	var stack []map[string]any
	var text []*strings.Builder
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Not the decoder's text: it can quote the document, and this
			// reason reaches events and replies.
			return nil, ErrNotXML
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(stack) == 0 && root != nil {
				return nil, fmt.Errorf("%w: more than one root element", ErrNotXML)
			}
			if len(stack) == MaxXMLDepth {
				return nil, fmt.Errorf("%w: elements nested deeper than %d", ErrNotXML, MaxXMLDepth)
			}
			el := map[string]any{}
			if t.Name.Space != "" {
				el["#ns"] = t.Name.Space
			}
			for _, a := range t.Attr {
				if a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns") {
					continue
				}
				el["@"+a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				root, rootName = el, t.Name.Local
			} else {
				parent := stack[len(stack)-1]
				children, _ := parent["#children"].([]any)
				parent["#children"] = append(children, el)
				if _, seen := parent[t.Name.Local]; !seen {
					parent[t.Name.Local] = el
				}
			}
			stack = append(stack, el)
			text = append(text, &strings.Builder{})
		case xml.EndElement:
			el, b := stack[len(stack)-1], text[len(text)-1]
			if s := b.String(); strings.TrimSpace(s) != "" {
				el["#text"] = s
			}
			stack, text = stack[:len(stack)-1], text[:len(text)-1]
		case xml.CharData:
			if len(text) > 0 {
				text[len(text)-1].Write(t)
			} else if len(bytes.TrimSpace(t)) > 0 {
				return nil, fmt.Errorf("%w: text outside the root element", ErrNotXML)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("%w: no root element", ErrNotXML)
	}
	return map[string]any{rootName: root}, nil
}
