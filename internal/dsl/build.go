package dsl

import (
	"encoding/json"
	"errors"
	"fmt"
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
	return buildStep{parts: parts, format: format}, nil
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
		return []byte(out), nil
	case FormatText:
		return []byte(fill(b.parts, doc, func(v string) string { return v })), nil
	}
	out := fill(b.parts, doc, escapeJSON)
	if !json.Valid([]byte(out)) {
		return nil, errors.New("the result is not valid JSON")
	}
	return []byte(out), nil
}

// escapeHL7 writes a value with HL7 escape sequences for the standard
// delimiters, and line breaks as hex escapes, so it stays one component.
var escapeHL7 = strings.NewReplacer(
	`\`, `\E\`, "|", `\F\`, "^", `\S\`, "~", `\R\`, "&", `\T\`,
	"\r", `\X0D\`, "\n", `\X0A\`,
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
