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

// negotiateXML answers with XML when the client prefers it (spec §5 "XML +
// JSON", #107 D-50): JSON responses are buffered and converted; every other
// response passes through unchanged.
func negotiateXML(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept")
		if !prefersXML(r.Header.Get("Accept")) {
			next.ServeHTTP(w, r)
			return
		}
		xw := &xmlWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(xw, r)
		xw.finish()
	})
}

// prefersXML reports whether the Accept header ranks application/xml or
// text/xml above application/json (and above */* when JSON is not named).
func prefersXML(accept string) bool {
	best := map[string]float64{}
	for _, part := range strings.Split(accept, ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		q := 1.0
		if v, ok := params["q"]; ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		switch mt {
		case "application/xml", "text/xml":
			mt = "xml"
		case "application/json", "*/*", "application/*":
			mt = "json" // JSON is what the server sends by default
		default:
			continue
		}
		if q > best[mt] {
			best[mt] = q
		}
	}
	return best["xml"] > 0 && best["xml"] > best["json"]
}

// xmlWriter buffers a JSON response to convert it; anything else is written
// straight through.
type xmlWriter struct {
	http.ResponseWriter
	status   int
	decided  bool
	convert  bool
	buffered bytes.Buffer
}

func (x *xmlWriter) WriteHeader(code int) {
	if x.decided {
		return
	}
	x.decided, x.status = true, code
	ct := x.Header().Get("Content-Type")
	x.convert = strings.HasPrefix(ct, "application/json")
	if !x.convert {
		x.ResponseWriter.WriteHeader(code)
	}
}

func (x *xmlWriter) Write(b []byte) (int, error) {
	if !x.decided {
		x.WriteHeader(http.StatusOK)
	}
	if x.convert {
		return x.buffered.Write(b)
	}
	return x.ResponseWriter.Write(b)
}

// finish writes the converted response (or the JSON unchanged if it cannot
// be converted).
func (x *xmlWriter) finish() {
	if !x.convert {
		return
	}
	body, err := jsonToXML(x.buffered.Bytes())
	h := x.Header()
	h.Del("Content-Length")
	if err != nil {
		x.ResponseWriter.WriteHeader(x.status)
		_, _ = x.ResponseWriter.Write(x.buffered.Bytes())
		return
	}
	h.Set("Content-Type", "application/xml; charset=utf-8")
	x.ResponseWriter.WriteHeader(x.status)
	_, _ = x.ResponseWriter.Write(body)
}

// xmlName matches keys that can be element names as they are.
var xmlName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9._-]*$`)

// jsonToXML converts one JSON document to XML (D-50): the root is
// <response>; an object's keys become child elements (or <entry key="…">
// when a key is not an element name); array elements become <item>;
// numbers, booleans, null, and arrays carry a type attribute; key order is
// kept.
func jsonToXML(doc []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var out bytes.Buffer
	out.WriteString(xml.Header)
	if err := writeXMLValue(&out, dec, "response", ""); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing data after the JSON document")
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// writeXMLValue writes the next JSON value as element name (with a key
// attribute when key is set).
func writeXMLValue(out *bytes.Buffer, dec *json.Decoder, name, key string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	open := func(typ string) {
		out.WriteString("<" + name)
		if key != "" {
			out.WriteString(` key="`)
			_ = xml.EscapeText(out, []byte(key))
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
				if err := writeXMLValue(out, dec, "item", ""); err != nil {
					return err
				}
			}
		} else {
			open("")
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return err
				}
				k, _ := kt.(string) // object keys are strings
				child, attr := k, ""
				if !xmlName.MatchString(k) || strings.HasPrefix(strings.ToLower(k), "xml") {
					child, attr = "entry", k
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
