package dsl

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/compiler"
)

// Output formats of the build step (#107 D-67).
const (
	FormatJSON  = "json"
	FormatHL7v2 = "hl7v2"
	FormatXML   = "xml"
	FormatText  = "text"
)

// buildStep renders the output from a template (#107 D-67): values are
// escaped for the format, and the result must be a valid document of it,
// so no value can change the output's structure.
type buildStep struct {
	parts  []templatePart
	format string
}

func compileBuild(b compiler.BuildStep) (step, error) {
	format := b.Format
	if format == "" {
		format = FormatJSON
	}
	switch format {
	case FormatJSON, FormatHL7v2, FormatXML, FormatText:
	default:
		return nil, fmt.Errorf("build.format must be json, hl7v2, xml, or text, got %q", b.Format)
	}
	if strings.TrimSpace(b.Template) == "" {
		return nil, errors.New("build.template is empty")
	}
	parts, err := compileTemplate(b.Template)
	if err != nil {
		return nil, fmt.Errorf("build.template: %w", err)
	}
	switch format {
	case FormatHL7v2:
		// Values are escaped for the standard delimiters, so the template
		// must declare them.
		if !strings.HasPrefix(strings.TrimLeft(b.Template, " \t\r\n"), `MSH|^~\&`) {
			return nil, errors.New(`build.template: an hl7v2 template must start with MSH|^~\& (the standard delimiters)`)
		}
	case FormatJSON:
		// Values are inserted as string content only: a placeholder outside
		// quotes could change the structure.
		if !placeholdersQuoted(parts) {
			return nil, errors.New(`build.template: in a json template every {{path}} must be inside a string ("{{path}}")`)
		}
	case FormatXML:
		// Escaping protects text and attribute values only; in a tag name,
		// comment, CDATA section, or declaration a value could still change
		// the structure.
		if !placeholdersInXMLValues(parts) {
			return nil, errors.New("build.template: in an xml template every {{path}} must be in element text or a quoted attribute value")
		}
	}
	return buildStep{parts: parts, format: format}, nil
}

// placeholdersInXMLValues reports whether every placeholder of an XML
// template sits in character data or inside a quoted attribute value.
func placeholdersInXMLValues(parts []templatePart) bool {
	const (
		text = iota
		tag
		attr
		other // comment, CDATA, processing instruction, or declaration
	)
	state, quote, end := text, byte(0), ""
	for _, part := range parts {
		if part.ref != nil {
			if state != text && state != attr {
				return false
			}
			continue
		}
		lit := part.literal
		for i := 0; i < len(lit); i++ {
			switch state {
			case text:
				if lit[i] != '<' {
					continue
				}
				rest := lit[i:]
				switch {
				case strings.HasPrefix(rest, "<!--"):
					state, end = other, "-->"
				case strings.HasPrefix(rest, "<![CDATA["):
					state, end = other, "]]>"
				case strings.HasPrefix(rest, "<?"):
					state, end = other, "?>"
				case strings.HasPrefix(rest, "<!"):
					state, end = other, ">"
				default:
					state = tag
				}
			case tag:
				switch lit[i] {
				case '"', '\'':
					state, quote = attr, lit[i]
				case '>':
					state = text
				}
			case attr:
				if lit[i] == quote {
					state = tag
				}
			case other:
				if strings.HasPrefix(lit[i:], end) {
					state, i = text, i+len(end)-1
				}
			}
		}
	}
	return true
}

// placeholdersQuoted reports whether every placeholder of a JSON template
// sits inside a string literal.
func placeholdersQuoted(parts []templatePart) bool {
	inString, escaped := false, false
	for _, part := range parts {
		if part.ref != nil {
			if !inString {
				return false
			}
			continue
		}
		for i := 0; i < len(part.literal); i++ {
			switch c := part.literal[i]; {
			case escaped:
				escaped = false
			case inString && c == '\\':
				escaped = true
			case c == '"':
				inString = !inString
			}
		}
	}
	return true
}

// apply never runs: Compile keeps the build step out of the step list.
func (buildStep) apply(map[string]any, map[string]bool) (bool, error) { return false, nil }

// render fills the template from doc and checks the result.
func (b buildStep) render(doc map[string]any) ([]byte, error) {
	switch b.format {
	case FormatHL7v2:
		out := fill(b.parts, doc, escapeHL7)
		// Segments may be written on lines; HL7 separates them with CR.
		out = strings.ReplaceAll(strings.ReplaceAll(out, "\r\n", "\n"), "\n", "\r")
		out = strings.Trim(out, "\r") + "\r"
		if _, err := codecs.HL7JSON([]byte(out)); err != nil {
			return nil, errors.New("the result is not an HL7 v2 message (MSH segment first)")
		}
		return []byte(out), nil
	case FormatXML:
		out := fill(b.parts, doc, escapeXML)
		if _, err := codecs.XMLJSON([]byte(out)); err != nil {
			return nil, errors.New("the result is not a well-formed XML document")
		}
		// The output is UTF-8; a declaration saying otherwise would make the
		// receiver misread it.
		if m := xmlEncoding.FindStringSubmatch(out); m != nil && !strings.EqualFold(m[1], "utf-8") {
			return nil, fmt.Errorf("the XML declaration says encoding %q; the output is UTF-8", m[1])
		}
		return []byte(out), nil
	case FormatText:
		return []byte(fill(b.parts, doc, func(v string) string { return v })), nil
	}
	out := fill(b.parts, doc, escapeJSON)
	var obj map[string]any
	if json.Unmarshal([]byte(out), &obj) != nil || obj == nil {
		return nil, errors.New("the result is not a JSON object")
	}
	return []byte(out), nil
}

// xmlEncoding finds the encoding an XML declaration names.
var xmlEncoding = regexp.MustCompile(`^\s*<\?xml[^>]*\bencoding\s*=\s*["']([^"']+)["']`)

// escapeHL7 writes a value with HL7 escape sequences for the standard
// delimiters, and line breaks and MLLP framing bytes as hex escapes, so it
// stays one component and can always be framed.
var escapeHL7 = strings.NewReplacer(
	`\`, `\E\`, "|", `\F\`, "^", `\S\`, "~", `\R\`, "&", `\T\`,
	"\r", `\X0D\`, "\n", `\X0A\`, "\x0b", `\X0B\`, "\x1c", `\X1C\`,
).Replace

// escapeXML writes a value as XML character data or attribute text.
var escapeXML = strings.NewReplacer(
	"&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;",
).Replace

// escapeJSON writes a value as the inside of a JSON string.
func escapeJSON(v string) string {
	b, _ := json.Marshal(v) // a string always encodes
	return string(b[1 : len(b)-1])
}
