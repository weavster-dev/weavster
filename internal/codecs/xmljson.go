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

// scanXML reads in as one well-formed, namespace-well-formed XML document
// and calls visit with each token in order, prefixes as written; for a
// StartElement, ns is the namespace scope inside it (prefix, "" for the
// default, -> URI), valid only during that call: the scan updates it in
// place. It refuses, with fixed words that never quote the
// document: an XML declaration that is not first; a directive other than
// one DOCTYPE before the root; more than one root; text outside it;
// unmatched end tags; undeclared prefixes; duplicate attributes; and more
// than MaxXMLDepth levels or MaxXMLElements elements.
//
// Safe by construction (D-14): encoding/xml processes no DTD and expands no
// entities beyond the predefined ones, and nothing is fetched. Documents
// may declare any IANA character set; a UTF-8 byte order mark is skipped.
func scanXML(in []byte, visit func(tok xml.Token, ns map[string]string) error) error {
	dec := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(in, []byte("\xEF\xBB\xBF"))))
	dec.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		enc, err := ianaindex.IANA.Encoding(label)
		if err != nil || enc == nil {
			return nil, fmt.Errorf("unsupported encoding %q", label)
		}
		return enc.NewDecoder().Reader(r), nil
	}
	// One scope map is updated in place: each element records only the
	// bindings it changed and restores them at its end tag. Copying the
	// inherited scope for every declaration made namespace-heavy documents
	// quadratic to read (#313).
	type binding struct {
		uri     string
		present bool
	}
	type open struct {
		name     xml.Name           // as written: Space is the prefix
		previous map[string]binding // what this element's declarations replaced
	}
	var stack []open
	scope := map[string]string{"xml": xmlNamespace}
	root := false
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
			return notXML("") // not the decoder's text: it can quote the document
		}
		tokens++
		ns := map[string]string(nil) // the scope visit sees: only for a start tag
		switch t := tok.(type) {
		case xml.ProcInst:
			// The XML declaration comes first or not at all.
			if strings.EqualFold(t.Target, "xml") && tokens != 1 {
				return notXML("")
			}
		case xml.Directive:
			// Only one DOCTYPE, before the root element (never processed).
			if !isDoctype(t) || doctype || root {
				return notXML("")
			}
			doctype = true
		case xml.StartElement:
			switch {
			case len(stack) == 0 && root:
				return notXML("more than one root element")
			case len(stack) == MaxXMLDepth:
				return notXML(fmt.Sprintf("elements nested deeper than %d", MaxXMLDepth))
			case elements == MaxXMLElements:
				return notXML(fmt.Sprintf("more than %d elements", MaxXMLElements))
			}
			elements++
			root = true
			var previous map[string]binding
			for _, a := range t.Attr {
				var prefix string
				switch {
				case a.Name.Space == "xmlns" && a.Value == "":
					return notXML("a namespace prefix bound to an empty URI")
				case a.Name.Space == "xmlns":
					prefix = a.Name.Local
				case a.Name.Space == "" && a.Name.Local == "xmlns":
					prefix = ""
				default:
					continue
				}
				if _, again := previous[prefix]; again {
					return notXML("duplicate attribute")
				}
				if previous == nil {
					previous = map[string]binding{}
				}
				uri, present := scope[prefix]
				previous[prefix] = binding{uri: uri, present: present}
				scope[prefix] = a.Value
			}
			// Pushed before the checks below, so a refusal leaves the
			// stack consistent (the scan stops at the error anyway).
			stack = append(stack, open{name: t.Name, previous: previous})
			if _, ok := scope[t.Name.Space]; t.Name.Space != "" && !ok {
				return notXML("undeclared namespace prefix")
			}
			seen := map[xml.Name]bool{} // attributes by expanded name: each once
			for _, a := range t.Attr {
				expanded := a.Name
				if a.Name.Space != "" && a.Name.Space != "xmlns" {
					uri, ok := scope[a.Name.Space]
					if !ok {
						return notXML("undeclared namespace prefix")
					}
					expanded.Space = "{" + uri + "}"
				}
				if seen[expanded] {
					return notXML("duplicate attribute")
				}
				seen[expanded] = true
			}
			ns = scope
		case xml.EndElement:
			if len(stack) == 0 || stack[len(stack)-1].name != t.Name {
				return notXML("")
			}
			for prefix, b := range stack[len(stack)-1].previous {
				if b.present {
					scope[prefix] = b.uri
				} else {
					delete(scope, prefix)
				}
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 && len(bytes.TrimSpace(t)) > 0 {
				return notXML("text outside the root element")
			}
		}
		if err := visit(tok, ns); err != nil {
			return err
		}
	}
	switch {
	case !root:
		return notXML("no root element")
	case len(stack) > 0:
		return notXML("")
	}
	return nil
}

// isDoctype reports whether a directive is a DOCTYPE declaration: the
// keyword, white space, and a name.
func isDoctype(d xml.Directive) bool {
	rest, ok := bytes.CutPrefix(d, []byte("DOCTYPE"))
	return ok && len(rest) > 1 && isXMLSpace(rest[0]) && len(bytes.TrimSpace(rest)) > 0
}

func isXMLSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// XMLJSON parses an XML document into the JSON view the DSL reads (#107
// D-62): {"<root local name>": element}. An element object has "@<name>"
// for each attribute, written as in the document ("@id", "@xml:lang",
// "@x:id"; namespace declarations left out), "#text" for its own text with
// surrounding whitespace trimmed (left out when empty), "#ns" for its
// namespace URI when it has one, and one key per child element's local
// name: the child's object, or a list of them in order when the name
// repeats. Every element appears exactly once, so the view grows with the
// document. The document is read by scanXML.
func XMLJSON(in []byte) (map[string]any, error) {
	type open struct {
		el   map[string]any
		text strings.Builder
	}
	var root map[string]any
	var rootName string
	var stack []*open
	err := scanXML(in, func(tok xml.Token, ns map[string]string) error {
		switch t := tok.(type) {
		case xml.StartElement:
			el := map[string]any{}
			if uri := ns[t.Name.Space]; uri != "" {
				el["#ns"] = uri
			}
			for _, a := range t.Attr {
				switch {
				case a.Name.Space == "xmlns" || (a.Name.Space == "" && a.Name.Local == "xmlns"):
				case a.Name.Space == "":
					el["@"+a.Name.Local] = a.Value
				default:
					el["@"+a.Name.Space+":"+a.Name.Local] = a.Value
				}
			}
			if len(stack) == 0 {
				root, rootName = el, t.Name.Local
			} else {
				addChild(stack[len(stack)-1].el, t.Name.Local, el)
			}
			stack = append(stack, &open{el: el})
		case xml.EndElement:
			o := stack[len(stack)-1]
			if s := strings.TrimSpace(o.text.String()); s != "" {
				o.el["#text"] = s
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text.Write(t)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
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
