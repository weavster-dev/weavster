package codecs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
)

// Limits of XMLJSON: how deeply elements nest and how many there are.
const (
	MaxXMLDepth    = 256
	MaxXMLElements = 100_000
)

// ErrNotXML reports input that is not a single well-formed XML document.
var ErrNotXML = errors.New("not a well-formed XML document")

func notXML(reason string) error { return &RefusedError{Err: ErrNotXML, Reason: reason} }

// xmlNamespace is the URI the xml: prefix always stands for.
const xmlNamespace = "http://www.w3.org/XML/1998/namespace"

// XMLJSON parses an XML document into the JSON view the DSL reads (#107
// D-62): {"<root local name>": element}. An element object has "@<name>"
// for each attribute, written as in the document ("@id", "@xml:lang",
// "@x:id"; namespace declarations left out), "#text" for its own text with
// surrounding whitespace trimmed (left out when empty), "#ns" for its
// namespace URI when it has one, and one key per child element's local
// name: the child's object, or a list of them in order when the name
// repeats. Every element appears exactly once, so the view grows with the
// document.
//
// Safe by construction (D-14): encoding/xml processes no DTD and expands no
// entities beyond the predefined ones, and nothing is fetched. Documents
// may declare any IANA character set; a UTF-8 byte order mark is skipped.
func XMLJSON(in []byte) (map[string]any, error) {
	dec := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(in, []byte("\xEF\xBB\xBF"))))
	dec.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		enc, err := ianaindex.IANA.Encoding(label)
		if err != nil || enc == nil {
			return nil, fmt.Errorf("unsupported encoding %q", label)
		}
		return enc.NewDecoder().Reader(r), nil
	}
	type binding struct {
		uri     string
		present bool
	}
	type open struct {
		name     xml.Name // as written: Space is the prefix
		el       map[string]any
		text     strings.Builder
		previous map[string]binding // only bindings changed by this element
	}
	var root map[string]any
	var rootName string
	var stack []*open
	ns := map[string]string{"xml": xmlNamespace}
	elements, tokens := 0, 0
	doctype := false
	for {
		// RawToken keeps prefixes as written; namespaces are resolved and
		// end tags matched here.
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, notXML("") // not the decoder's text: it can quote the document
		}
		tokens++
		switch t := tok.(type) {
		case xml.ProcInst:
			// The XML declaration comes first or not at all.
			if strings.EqualFold(t.Target, "xml") && tokens != 1 {
				return nil, notXML("")
			}
		case xml.Directive:
			// Only one DOCTYPE, before the root element (never processed).
			if !bytes.HasPrefix(t, []byte("DOCTYPE")) || doctype || root != nil {
				return nil, notXML("")
			}
			doctype = true
		case xml.StartElement:
			switch {
			case len(stack) == 0 && root != nil:
				return nil, notXML("more than one root element")
			case len(stack) == MaxXMLDepth:
				return nil, notXML(fmt.Sprintf("elements nested deeper than %d", MaxXMLDepth))
			case elements == MaxXMLElements:
				return nil, notXML(fmt.Sprintf("more than %d elements", MaxXMLElements))
			}
			elements++
			// Update bindings in place, saving only this element's changes.
			// Copying the whole inherited map per declaration or element
			// makes small namespace-heavy documents quadratic to parse.
			var previous map[string]binding
			for _, a := range t.Attr {
				var prefix string
				switch {
				case a.Name.Space == "xmlns":
					prefix = a.Name.Local
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					prefix = ""
				default:
					continue
				}
				if previous == nil {
					previous = make(map[string]binding)
				}
				if _, duplicate := previous[prefix]; duplicate {
					return nil, notXML("duplicate attribute")
				}
				uri, present := ns[prefix]
				previous[prefix] = binding{uri: uri, present: present}
				ns[prefix] = a.Value
			}
			uri, ok := ns[t.Name.Space]
			if t.Name.Space != "" && !ok {
				return nil, notXML("undeclared namespace prefix")
			}
			el := map[string]any{}
			if uri != "" {
				el["#ns"] = uri
			}
			seen := map[xml.Name]bool{} // attributes by expanded name: each once
			for _, a := range t.Attr {
				expanded := a.Name
				if a.Name.Space != "" && a.Name.Space != "xmlns" {
					expanded.Space = "{" + ns[a.Name.Space] + "}"
				}
				if seen[expanded] {
					return nil, notXML("duplicate attribute")
				}
				seen[expanded] = true
			}
			for _, a := range t.Attr {
				switch {
				case a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns"):
				case a.Name.Space == "":
					el["@"+a.Name.Local] = a.Value
				default:
					if _, ok := ns[a.Name.Space]; !ok {
						return nil, notXML("undeclared namespace prefix")
					}
					el["@"+a.Name.Space+":"+a.Name.Local] = a.Value
				}
			}
			if len(stack) == 0 {
				root, rootName = el, t.Name.Local
			} else {
				addChild(stack[len(stack)-1].el, t.Name.Local, el)
			}
			stack = append(stack, &open{name: t.Name, el: el, previous: previous})
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != t.Name {
				return nil, notXML("")
			}
			o := stack[len(stack)-1]
			if s := strings.TrimSpace(o.text.String()); s != "" {
				o.el["#text"] = s
			}
			for prefix, old := range o.previous {
				if old.present {
					ns[prefix] = old.uri
				} else {
					delete(ns, prefix)
				}
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			} else if len(bytes.TrimSpace(t)) > 0 {
				return nil, notXML("text outside the root element")
			}
		}
	}
	if len(stack) > 0 || root == nil {
		if root == nil {
			return nil, notXML("no root element")
		}
		return nil, notXML("")
	}
	return map[string]any{rootName: root}, nil
}

// addChild adds el under name: the first one as an object, then a list.
func addChild(parent map[string]any, name string, el map[string]any) {
	switch prev := parent[name].(type) {
	case nil:
		parent[name] = el
	case []any:
		parent[name] = append(prev, el)
	default:
		parent[name] = []any{prev, el}
	}
}
