package gateway

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// negotiateXML marks the response for XML when the client prefers it (spec
// §5 "XML + JSON", #107 D-50). Only what the API itself encodes (writeJSON:
// resources, lists, and error envelopes) is converted; message content,
// exports of other formats, and the OpenAPI document pass through unchanged.
func negotiateXML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")
		if prefersXML(strings.Join(r.Header.Values("Accept"), ",")) {
			w = xmlResponse{w}
		}
		next.ServeHTTP(w, r)
	})
}

// xmlResponse marks a response writer whose client asked for XML.
type xmlResponse struct{ http.ResponseWriter }

func (x xmlResponse) Unwrap() http.ResponseWriter { return x.ResponseWriter }

// wantsXML reports whether w (or a writer it wraps) was marked for XML.
func wantsXML(w http.ResponseWriter) bool {
	for {
		if _, ok := w.(xmlResponse); ok {
			return true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
}

// prefersXML reports whether the Accept header explicitly asks for XML: a
// range naming application/xml or text/xml has the highest quality of every
// listed range and a higher quality than application/json. Each type's quality comes from
// its most specific matching range (RFC 9110 §12.5.1); a range with a
// malformed q is ignored. A browser's Accept (text/html first, XML at 0.9)
// therefore gets JSON.
func prefersXML(accept string) bool {
	type rng struct {
		typ, sub string
		q        float64
	}
	var ranges []rng
	top := 0.0
	for _, part := range strings.Split(accept, ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		q := 1.0
		if v, ok := params["q"]; ok {
			if q, err = strconv.ParseFloat(v, 64); err != nil || q < 0 || q > 1 {
				continue
			}
		}
		typ, sub, _ := strings.Cut(mt, "/")
		ranges = append(ranges, rng{typ, sub, q})
		top = max(top, q)
	}
	// quality is typ/sub's quality and how specific its range is (3 exact,
	// 2 type/*, 1 */*, 0 unmatched).
	quality := func(typ, sub string) (float64, int) {
		q, rank := 0.0, 0
		for _, r := range ranges {
			n := 0
			switch {
			case r.typ == typ && r.sub == sub:
				n = 3
			case r.typ == typ && r.sub == "*":
				n = 2
			case r.typ == "*" && r.sub == "*":
				n = 1
			}
			if n > rank {
				q, rank = r.q, n
			}
		}
		return q, rank
	}
	xmlQ := 0.0 // only a range that names an XML type counts
	for _, sub := range [][2]string{{"application", "xml"}, {"text", "xml"}} {
		if q, rank := quality(sub[0], sub[1]); rank == 3 {
			xmlQ = max(xmlQ, q)
		}
	}
	jsonQ, _ := quality("application", "json")
	return xmlQ > 0 && xmlQ == top && xmlQ > jsonQ
}

// writeXML writes v as XML; it reports false (writing nothing) when v cannot
// be encoded.
func writeXML(w http.ResponseWriter, status int, v any) bool {
	doc, err := json.Marshal(v)
	if err != nil {
		return false
	}
	body, err := jsonToXML(doc)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return true
}

// xmlName matches keys that can be element names as they are.
var xmlName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]*$`)

// jsonToXML converts one JSON document to XML (D-50): the root is
// <response>; an object's keys become child elements (or <entry key="…">
// when a key is not an element name); array elements become <item>;
// numbers, booleans, null, arrays, and empty objects carry a type
// attribute; key order is kept.
func jsonToXML(doc []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var out bytes.Buffer
	out.WriteString(xml.Header)
	if err := writeXMLValue(&out, dec, "response", nil); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON document")
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// writeXMLValue writes the next JSON value as element name (with a key
// attribute when key is non-nil).
func writeXMLValue(out *bytes.Buffer, dec *json.Decoder, name string, key *string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	open := func(typ string) {
		out.WriteString("<" + name)
		if key != nil {
			out.WriteString(` key="`)
			_ = xml.EscapeText(out, []byte(*key))
			out.WriteByte('"')
		}
		if typ != "" {
			out.WriteString(` type="` + typ + `"`)
		}
		out.WriteByte('>')
	}
	closeTag := "</" + name + ">"
	switch v := tok.(type) {
	case json.Delim:
		if v == '[' {
			open("array")
			for dec.More() {
				if err := writeXMLValue(out, dec, "item", nil); err != nil {
					return err
				}
			}
		} else {
			if dec.More() {
				open("")
			} else {
				open("object") // tells {} apart from ""
			}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				k, _ := kt.(string) // object keys are strings
				child, attr := k, (*string)(nil)
				if !xmlName.MatchString(k) || strings.HasPrefix(strings.ToLower(k), "xml") {
					child, attr = "entry", &k
				}
				if err := writeXMLValue(out, dec, child, attr); err != nil {
					return err
				}
			}
		}
		if _, err := dec.Token(); err != nil { // the closing ] or }
			return err
		}
	case string:
		open("")
		_ = xml.EscapeText(out, []byte(v))
	case json.Number:
		open("number")
		out.WriteString(v.String())
	case bool:
		open("boolean")
		out.WriteString(strconv.FormatBool(v))
	case nil:
		open("null")
	}
	out.WriteString(closeTag)
	return nil
}
