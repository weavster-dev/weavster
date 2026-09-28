// Package flowdef is the single flow-definition model: the JSON document
// the flow API accepts and returns, and the flows of a config-as-code
// document, with its JSON Schema (flow.schema.json) and validation.
package flowdef

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/weavster-dev/weavster/internal/compiler"
)

// Flow is a flow definition. Status and StoppedDestinations are runtime
// state: the API reports them, but definitions (create, update, import,
// config-as-code) must not set them.
type Flow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
	// Source is where the flow reads messages on its own (nil: only the
	// API sends it messages).
	Source  *Source `json:"source,omitempty"`
	Status  string  `json:"status,omitempty"`
	Enabled bool    `json:"enabled"`
	// InitialState is the status automatic deployment at server start
	// gives the flow: started (when empty), paused, or stopped.
	InitialState string `json:"initialState,omitempty"`
	// DependsOn lists flows this flow requires (ids); kept acyclic.
	DependsOn []string `json:"dependsOn,omitempty"`
	// StoppedDestinations is runtime state (like Status): destinations held
	// by POST .../destinations/{name}/stop.
	StoppedDestinations []string `json:"stoppedDestinations,omitempty"`
	// Transform is the flow's YAML DSL transform as a JSON object (the
	// transform.schema.json shape), kept unparsed.
	Transform    json.RawMessage `json:"transform,omitempty"`
	Destinations []Destination   `json:"destinations,omitempty"`
	// ResponseSelector names the destination whose reply is returned to
	// the sender of a message.
	ResponseSelector string `json:"responseSelector,omitempty"`
	// InputFormat is how transforms read a message: json (default), or the
	// JSON view of an HL7 v2 message (hl7v2), XML document (xml), or
	// delimited text (delimited) (#107 D-61 to D-63).
	InputFormat string `json:"inputFormat,omitempty"`
	// Delimited configures inputFormat delimited (#107 D-63).
	Delimited *Delimited `json:"delimited,omitempty"`
}

// Delimited describes delimited text: the delimiter ("," when empty) and
// whether the first row names the columns (true when nil).
type Delimited struct {
	Delimiter string `json:"delimiter,omitempty"`
	Header    *bool  `json:"header,omitempty"`
}

// CheckTransforms checks what the schema cannot about transforms (#107
// D-66, D-67): destinationSet steps belong in the flow's transform only,
// every destination they exclude is a destination of the flow, and a
// response transform has no build step.
func CheckTransforms(f Flow) error {
	names := map[string]bool{}
	for _, d := range f.Destinations {
		names[d.Name] = true
		for field, raw := range map[string]json.RawMessage{"transform": d.Transform, "responseTransform": d.ResponseTransform} {
			if len(excluded(raw)) > 0 {
				return fmt.Errorf("destination %s: %s: destinationSet can only be used in the flow's transform", d.Name, field)
			}
		}
		if hasBuild(d.ResponseTransform) {
			return fmt.Errorf("destination %s: responseTransform: build cannot be used here: the reply returned to the sender is JSON", d.Name)
		}
	}
	for _, name := range excluded(f.Transform) {
		if !names[name] {
			return fmt.Errorf("transform: destinationSet excludes %q, which is not a destination of the flow", name)
		}
	}
	return nil
}

// hasBuild reports whether a transform has a build step anywhere.
func hasBuild(raw json.RawMessage) bool {
	var t compiler.Transform
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return false
	}
	for _, s := range t.Steps {
		if s.Build != nil {
			return true
		}
	}
	return false
}

// excluded lists the names a transform's destinationSet steps exclude
// (none when raw is empty or not a transform; the schema reports that).
func excluded(raw json.RawMessage) []string {
	var t compiler.Transform
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return nil
	}
	var out []string
	for _, s := range t.Steps {
		if s.DestinationSet != nil {
			out = append(out, s.DestinationSet.Exclude...)
		}
	}
	return out
}

// CheckDestinations checks what the schema cannot about destinations: a
// file destination's dir is an absolute path, so where files go never
// depends on the server's working directory (#107 D-69).
func CheckDestinations(f Flow) error {
	for _, d := range f.Destinations {
		// An empty dir is reported as required when the flow is used.
		if d.Type == "file" && d.Dir != "" && !filepath.IsAbs(d.Dir) {
			return fmt.Errorf("destination %s: dir must be an absolute path, got %q", d.Name, d.Dir)
		}
	}
	return nil
}

// CheckInput checks what the schema cannot: delimited options go with
// inputFormat delimited only.
func CheckInput(f Flow) error {
	if f.Delimited != nil && f.InputFormat != "delimited" {
		return errors.New("delimited applies only to inputFormat delimited")
	}
	return nil
}

// SourceKind is the flow's source type: its source's, or else the
// free-text sourceType.
func (f Flow) SourceKind() string {
	if f.Source != nil {
		return f.Source.Type
	}
	return f.SourceType
}

// Source is a flow's own message source (#107 D-56).
type Source struct {
	Type string `json:"type"` // file, http, or mllp
	// Dir is the absolute directory a file source polls.
	Dir string `json:"dir,omitempty"`
	// Pattern is a file-name glob (default "*").
	Pattern string `json:"pattern,omitempty"`
	// PollIntervalMs is how often the directory is read (default 1000).
	PollIntervalMs int `json:"pollIntervalMs,omitempty"`
	// MoveTo is an absolute directory processed files are moved to
	// (default: they are deleted).
	MoveTo string `json:"moveTo,omitempty"`
	// Recursive makes a file source read subdirectories of Dir too (#107
	// D-69).
	Recursive bool `json:"recursive,omitempty"`
	// Address is the host:port an http or mllp source listens on (#107
	// D-57, D-60).
	Address string `json:"address,omitempty"`
	// Path is the request path an http source accepts (default "/").
	Path string `json:"path,omitempty"`
	// Method is the request method an http source accepts (default POST).
	Method string `json:"method,omitempty"`
	// Username and PasswordEnv require HTTP Basic credentials on an http
	// source; the password is read from the server's environment variable
	// PasswordEnv when the port opens (#107 D-58).
	Username    string `json:"username,omitempty"`
	PasswordEnv string `json:"passwordEnv,omitempty"`
	// CertFile and KeyFile make an http source serve HTTPS.
	CertFile string `json:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`
	// ReadTimeoutMs bounds reading one request on an http source
	// (default 60000).
	ReadTimeoutMs int `json:"readTimeoutMs,omitempty"`
}

// Destination is one delivery target of a flow.
type Destination struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Dir  string `json:"dir,omitempty"`
	// Address is the host:port an mllp destination delivers to (#107 D-64).
	Address string `json:"address,omitempty"`
	// Method, TimeoutMs, and MaxRedirects shape an http destination's
	// request: POST, 30 s, and no redirects followed by default (#107 D-59).
	Method       string `json:"method,omitempty"`
	TimeoutMs    int    `json:"timeoutMs,omitempty"`
	MaxRedirects int    `json:"maxRedirects,omitempty"`
	// Transform is this destination's own DSL transform (filter steps
	// included), applied to the flow's output; kept unparsed.
	Transform json.RawMessage `json:"transform,omitempty"`
	// ResponseTransform is applied to this destination's reply.
	ResponseTransform json.RawMessage `json:"responseTransform,omitempty"`
}

// CheckSource checks what the schema cannot: a file source's directories
// are absolute and distinct, and its pattern is a valid file-name glob; an
// http source's address is host:port with a non-zero port.
func CheckSource(s *Source) error {
	if s == nil {
		return nil
	}
	if s.Type == "mllp" {
		if *s != (Source{Type: "mllp", Address: s.Address}) {
			return errors.New("an mllp source takes only type and address")
		}
		_, err := SourcePort(s)
		return err
	}
	if s.Type == "http" {
		switch {
		case (s.Username == "") != (s.PasswordEnv == ""):
			return errors.New("source.username and source.passwordEnv go together")
		case (s.CertFile == "") != (s.KeyFile == ""):
			return errors.New("source.certFile and source.keyFile go together")
		case s.CertFile != "" && (!filepath.IsAbs(s.CertFile) || !filepath.IsAbs(s.KeyFile)):
			return errors.New("source.certFile and source.keyFile must be absolute paths")
		}
		_, err := SourcePort(s)
		return err
	}
	switch {
	case !filepath.IsAbs(s.Dir):
		return fmt.Errorf("source.dir must be an absolute path, got %q", s.Dir)
	case s.MoveTo != "" && !filepath.IsAbs(s.MoveTo):
		return fmt.Errorf("source.moveTo must be an absolute path, got %q", s.MoveTo)
	case s.MoveTo != "" && filepath.Clean(s.MoveTo) == filepath.Clean(s.Dir):
		return errors.New("source.moveTo must differ from source.dir")
	case s.MoveTo != "" && filepath.Join(s.MoveTo, "rejected") == filepath.Clean(s.Dir):
		return errors.New("source.dir must not be moveTo/rejected, where refused files are moved")
	case s.Recursive && s.MoveTo != "" && Within(s.MoveTo, s.Dir):
		return errors.New("source.moveTo must not be inside source.dir when recursive: moved files would be read again")
	case strings.ContainsAny(s.Pattern, `/\`):
		return fmt.Errorf("source.pattern is a file-name glob without path separators, got %q", s.Pattern)
	}
	if _, err := filepath.Match(s.Pattern, "x"); err != nil {
		return fmt.Errorf("source.pattern %q: %w", s.Pattern, err)
	}
	return nil
}

// Within reports whether path is dir or inside it.
func Within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Listens reports whether s is a source with its own port (http or mllp).
func (s *Source) Listens() bool {
	return s != nil && (s.Type == "http" || s.Type == "mllp")
}

// SourcePort is the port an http or mllp source listens on.
func SourcePort(s *Source) (int, error) {
	_, p, err := net.SplitHostPort(s.Address)
	if err != nil {
		return 0, fmt.Errorf("source.address must be host:port, got %q", s.Address)
	}
	port, err := strconv.Atoi(p)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("source.address needs a port from 1 to 65535, got %q", s.Address)
	}
	return port, nil
}
