// Package codecs implements data-type codecs: delimited, HL7 v2, X12, NCPDP,
// JSON, XML, raw, with acknowledgment generation where applicable.
package codecs

import (
	"errors"
	"fmt"
	"sort"
)

// Sentinel errors returned by codecs.
var (
	// ErrNotSupported indicates the codec does not implement the operation
	// (e.g. acknowledgment generation).
	ErrNotSupported = errors.New("codec: operation not supported")

	// ErrEnterprise indicates the operation requires a licensed,
	// Enterprise-scoped codec (e.g. DICOM).
	ErrEnterprise = errors.New("codec: enterprise feature requires a licensed library")
)

// Codec is the port implemented by every data-type codec: it parses a
// serialized payload into a structured value and serializes it back.
type Codec interface {
	// Name returns the registered codec name (e.g. "hl7v2", "json").
	Name() string
	// Parse decodes a serialized payload into a structured value.
	Parse(in []byte) (any, error)
	// Serialize encodes a structured value back into a serialized payload.
	Serialize(v any) ([]byte, error)
	// Acknowledge generates a protocol acknowledgment for the message.
	// Codecs without acknowledgment semantics return ErrNotSupported.
	Acknowledge(in []byte) ([]byte, error)
}

// Registry maps codec names to implementations.
type Registry struct {
	m map[string]Codec
}

// NewRegistry returns a registry seeded with the given codecs.
func NewRegistry(codecs ...Codec) *Registry {
	r := &Registry{m: make(map[string]Codec, len(codecs))}
	for _, c := range codecs {
		r.Register(c)
	}
	return r
}

// Register adds a codec, keyed by its Name.
func (r *Registry) Register(c Codec) {
	if r.m == nil {
		r.m = make(map[string]Codec)
	}
	r.m[c.Name()] = c
}

// Get returns the codec registered under name.
func (r *Registry) Get(name string) (Codec, error) {
	c, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("codec: unknown codec %q", name)
	}
	return c, nil
}

// Names returns the registered codec names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.m))
	for n := range r.m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Standard returns the registry of all MVP codecs.
func Standard() *Registry {
	return NewRegistry(
		JSON(), XML(), Raw(),
		NewDelimited('|', false),
		HL7v2(), X12(), NCPDP(),
	)
}

// CoverageEntry documents a codec's support level (gap #12).
type CoverageEntry struct {
	Name           string
	Versions       string
	Acknowledgment bool
	Enterprise     bool
	Notes          string
}

// CoverageMatrix returns the explicit codec coverage matrix (gap #12).
func CoverageMatrix() []CoverageEntry {
	return []CoverageEntry{
		{Name: "delimited", Versions: "any (configurable delimiter)", Notes: "tab/pipe/comma; optional header"},
		{Name: "hl7v2", Versions: "2.x segment/field/component/repetition", Acknowledgment: true, Notes: "MSH/MSA ACK"},
		{Name: "json", Versions: "RFC 8259", Notes: "stdlib encoding/json"},
		{Name: "xml", Versions: "XML 1.0 (XXE-safe)", Notes: "no DTD/external-entity resolution by construction"},
		{Name: "x12", Versions: "ISA/GS/ST envelope", Acknowledgment: true, Notes: "997 functional acknowledgment"},
		{Name: "ncpdp", Versions: "Telecommunication (FS/GS/RS delimiters)", Notes: "fixed-width amount formatting; response limited"},
		{Name: "raw", Versions: "any binary", Notes: "passthrough"},
		{Name: "dicom", Enterprise: true, Notes: "requires a licensed library; interface stub only (gap #12)"},
	}
}

// dicomCodec is the Enterprise-scoped DICOM codec stub (gap #12, arch §9.2).
type dicomCodec struct{}

// DICOM returns the Enterprise DICOM codec stub; every operation returns
// ErrEnterprise until a licensed library is wired in.
func DICOM() Codec { return dicomCodec{} }

func (dicomCodec) Name() string              { return "dicom" }
func (dicomCodec) Parse([]byte) (any, error) { return nil, ErrEnterprise }
func (dicomCodec) Serialize(any) ([]byte, error) {
	return nil, ErrEnterprise
}
func (dicomCodec) Acknowledge([]byte) ([]byte, error) { return nil, ErrEnterprise }
