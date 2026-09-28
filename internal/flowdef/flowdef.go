// Package flowdef is the single flow-definition model: the JSON document
// the flow API accepts and returns, and the flows of a config-as-code
// document, with its JSON Schema (flow.schema.json) and validation.
package flowdef

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/dsl"
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

// Dependencies lists the flows f needs: its dependsOn, then the targets
// of its flow destinations (#107 D-70), each once.
func (f Flow) Dependencies() []string {
	out := append([]string(nil), f.DependsOn...)
	for _, d := range f.Destinations {
		if d.Type == "flow" && d.Flow != "" && !slices.Contains(out, d.Flow) {
			out = append(out, d.Flow)
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
		switch {
		case d.Type == "file" && d.Dir != "" && !filepath.IsAbs(d.Dir):
			return fmt.Errorf("destination %s: dir must be an absolute path, got %q", d.Name, d.Dir)
		case (d.TLS || d.CAFile != "") && d.Type != "mllp":
			return fmt.Errorf("destination %s: tls and caFile apply only to mllp destinations", d.Name)
		case d.CAFile != "" && !d.TLS:
			return fmt.Errorf("destination %s: caFile needs tls: true", d.Name)
		case d.CAFile != "" && !filepath.IsAbs(d.CAFile):
			return fmt.Errorf("destination %s: caFile must be an absolute path, got %q", d.Name, d.CAFile)
		case (d.FrameStart != "" || d.FrameEnd != "" || d.AckMode != "") && d.Type != "mllp":
			return fmt.Errorf("destination %s: frameStart, frameEnd, and ackMode apply only to mllp destinations", d.Name)
		}
		if err := checkMLLPMode(d.FrameStart, d.FrameEnd, d.AckMode); err != nil {
			return fmt.Errorf("destination %s: %w", d.Name, err)
		}
		if err := checkDatabase(d); err != nil {
			return fmt.Errorf("destination %s: %w", d.Name, err)
		}
	}
	return nil
}

// dsnEnvName is the environment variable a database destination may read
// its connection string from (only these, so a flows:edit user cannot use
// the server's other secrets).
var dsnEnvName = regexp.MustCompile(`^WEAVSTER_DB_[A-Z0-9_]+$`)

// checkDatabase checks a database destination (#107 D-75): driver, dsnEnv,
// table and column identifiers, and non-empty paths; the fields are
// refused on other types.
func checkDatabase(d Destination) error {
	fields := d.Driver != "" || d.DSNEnv != "" || d.Table != "" || len(d.Columns) > 0 || d.KeyColumn != ""
	switch {
	case d.Type != "database" && fields:
		return errors.New("driver, dsnEnv, table, columns, and keyColumn apply only to database destinations")
	case d.Type != "database":
		return nil
	case d.Driver != "postgres" && d.Driver != "sqlite":
		return fmt.Errorf("driver must be postgres or sqlite, got %q", d.Driver)
	case !dsnEnvName.MatchString(d.DSNEnv):
		return fmt.Errorf("dsnEnv must name an environment variable WEAVSTER_DB_..., got %q", d.DSNEnv)
	case !adapters.ValidSQLIdentifier(d.Table, true):
		return fmt.Errorf("table must be a name or schema.name of letters, digits, and _, got %q", d.Table)
	case len(d.Columns) == 0:
		return errors.New("columns must map at least one column to a path in the message")
	case d.KeyColumn != "" && !adapters.ValidSQLIdentifier(d.KeyColumn, false):
		return fmt.Errorf("keyColumn must be a column name of letters, digits, and _, got %q", d.KeyColumn)
	}
	for col, path := range d.Columns {
		switch {
		case !adapters.ValidSQLIdentifier(col, false):
			return fmt.Errorf("column %q must be a name of letters, digits, and _", col)
		case col == d.KeyColumn:
			return fmt.Errorf("column %q is the keyColumn, which gets the idempotency key", col)
		case dsl.CheckPath(path) != nil:
			return fmt.Errorf("column %s: path must be dot-separated names, got %q", col, path)
		}
	}
	return nil
}

// checkSchedule checks a source's schedule (#107 D-77): a standard cron
// expression or descriptor, on a file or database source, not together
// with pollIntervalMs.
func checkSchedule(s *Source) error {
	switch {
	case s.Schedule == "":
		return nil
	case s.Type != "file" && s.Type != "database":
		return errors.New("source.schedule applies only to file and database sources")
	case s.PollIntervalMs != 0:
		return errors.New("source.schedule and source.pollIntervalMs cannot both be set")
	}
	sched, err := cron.ParseStandard(s.Schedule)
	if err != nil {
		return fmt.Errorf("source.schedule %q is not a cron expression (minute hour day-of-month month day-of-week, or @hourly, @daily, @every 30s, …; optionally CRON_TZ=Area/City first)", s.Schedule)
	}
	if every, ok := sched.(cron.ConstantDelaySchedule); ok {
		// robfig rounds anything shorter up to a second without saying so.
		if d, err := time.ParseDuration(strings.TrimSpace(s.Schedule[strings.Index(s.Schedule, "@every")+len("@every"):])); err != nil || d < time.Second || d != every.Delay {
			return fmt.Errorf("source.schedule %q: @every takes whole seconds of at least 1s (use pollIntervalMs for shorter intervals)", s.Schedule)
		}
	}
	if sched.Next(time.Now()).IsZero() {
		return fmt.Errorf("source.schedule %q never runs (no such date)", s.Schedule)
	}
	return nil
}

// checkDatabaseSource checks a database source (#107 D-76).
func checkDatabaseSource(s *Source) error {
	other := Source{Type: s.Type, Driver: s.Driver, DSNEnv: s.DSNEnv, Query: s.Query, IDColumn: s.IDColumn, Update: s.Update,
		MaxRows: s.MaxRows, TimeoutMs: s.TimeoutMs, PollIntervalMs: s.PollIntervalMs, Schedule: s.Schedule}
	switch {
	case *s != other:
		return errors.New("a database source takes only type, driver, dsnEnv, query, idColumn, update, pollIntervalMs or schedule, maxRows, and timeoutMs")
	case s.Driver != "postgres" && s.Driver != "sqlite":
		return fmt.Errorf("source.driver must be postgres or sqlite, got %q", s.Driver)
	case !dsnEnvName.MatchString(s.DSNEnv):
		return fmt.Errorf("source.dsnEnv must name an environment variable WEAVSTER_DB_..., got %q", s.DSNEnv)
	case !adapters.ValidSelect(s.Query):
		return errors.New("source.query must be one SELECT or WITH statement (no ; inside it)")
	case s.IDColumn == "":
		return errors.New("source.idColumn is required: the column of the query's result that identifies a row")
	case s.Update == nil:
		return errors.New("source.update is required: it marks each row once stored, and the query must leave marked rows out")
	}
	u := s.Update
	switch {
	case !adapters.ValidSQLIdentifier(u.Table, true):
		return fmt.Errorf("source.update.table must be a name or schema.name of letters, digits, and _, got %q", u.Table)
	case !adapters.ValidSQLIdentifier(u.Key, false):
		return fmt.Errorf("source.update.key must be a column name of letters, digits, and _, got %q", u.Key)
	case len(u.Set) == 0:
		return errors.New("source.update.set must give at least one column a value")
	}
	for col := range u.Set {
		if !adapters.ValidSQLIdentifier(col, false) {
			return fmt.Errorf("source.update.set: column %q must be a name of letters, digits, and _", col)
		}
	}
	return nil
}

// MLLPFraming decodes an mllp source's or destination's frameStart and
// frameEnd (hex; defaults 0B and 1C0D, MLLP's VT and FS CR).
func MLLPFraming(start, end string) (byte, []byte, error) {
	if start == "" {
		start = "0B"
	}
	if end == "" {
		end = "1C0D"
	}
	s, err := hex.DecodeString(start)
	if err != nil || len(s) != 1 {
		return 0, nil, fmt.Errorf("frameStart must be one byte in hex, such as 0B, got %q", start)
	}
	e, err := hex.DecodeString(end)
	if err != nil || len(e) < 1 || len(e) > 2 {
		return 0, nil, fmt.Errorf("frameEnd must be one or two bytes in hex, such as 1C0D, got %q", end)
	}
	return s[0], e, nil
}

// checkMLLPMode checks framing and ackMode: the start byte and the first
// end byte must be control bytes that HL7 v2 text never contains (not tab,
// line feed, or carriage return), and differ.
func checkMLLPMode(start, end, ackMode string) error {
	s, e, err := MLLPFraming(start, end)
	switch {
	case err != nil:
		return err
	case !frameByte(s):
		return fmt.Errorf("frameStart %02X can occur in a message; use a control byte such as 0B", s)
	case !frameByte(e[0]):
		return fmt.Errorf("frameEnd starts with %02X, which can occur in a message; use a control byte such as 1C", e[0])
	case s == e[0]:
		return errors.New("frameStart and the first byte of frameEnd must differ")
	case len(e) == 2 && e[0] == e[1]:
		// A message ending in that byte could not be told from the end.
		return errors.New("the two bytes of frameEnd must differ")
	case ackMode != "" && ackMode != "original" && ackMode != "none":
		return fmt.Errorf("ackMode must be original or none, got %q", ackMode)
	}
	return nil
}

// frameByte reports whether b is a control byte other than tab, line
// feed, and carriage return.
func frameByte(b byte) bool {
	return (b < 0x20 && b != '\t' && b != '\n' && b != '\r') || b == 0x7f
}

// CheckInput checks what the schema cannot: delimited options go with
// inputFormat delimited only, and a database source with JSON input.
func CheckInput(f Flow) error {
	if f.Delimited != nil && f.InputFormat != "delimited" {
		return errors.New("delimited applies only to inputFormat delimited")
	}
	if f.Source != nil && f.Source.Type == "database" && f.InputFormat != "" && f.InputFormat != "json" {
		return errors.New("a database source sends JSON messages (one object per row): inputFormat must be json")
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
	Type string `json:"type"` // file, http, mllp, or database
	// Dir is the absolute directory a file source polls.
	Dir string `json:"dir,omitempty"`
	// Pattern is a file-name glob (default "*").
	Pattern string `json:"pattern,omitempty"`
	// PollIntervalMs is how often the directory is read (default 1000).
	PollIntervalMs int `json:"pollIntervalMs,omitempty"`
	// Schedule polls a file or database source at cron times instead of
	// every PollIntervalMs (#107 D-77).
	Schedule string `json:"schedule,omitempty"`
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
	// CertFile and KeyFile make an http source serve HTTPS, and an mllp
	// source accept MLLP over TLS (#107 D-71).
	CertFile string `json:"certFile,omitempty"`
	KeyFile  string `json:"keyFile,omitempty"`
	// ReadTimeoutMs bounds reading one request on an http source
	// (default 60000).
	ReadTimeoutMs int `json:"readTimeoutMs,omitempty"`
	// FrameStart, FrameEnd, and AckMode set an mllp source's framing and
	// whether it answers with ACKs (#107 D-72).
	FrameStart string `json:"frameStart,omitempty"`
	FrameEnd   string `json:"frameEnd,omitempty"`
	AckMode    string `json:"ackMode,omitempty"`
	// Driver, DSNEnv, Query, IDColumn, Update, MaxRows, and TimeoutMs
	// describe a database source (#107 D-76): Query runs read-only every
	// PollIntervalMs, each row becomes a message, and Update marks it.
	Driver    string        `json:"driver,omitempty"`
	DSNEnv    string        `json:"dsnEnv,omitempty"`
	Query     string        `json:"query,omitempty"`
	IDColumn  string        `json:"idColumn,omitempty"`
	Update    *SourceUpdate `json:"update,omitempty"`
	MaxRows   int           `json:"maxRows,omitempty"`
	TimeoutMs int           `json:"timeoutMs,omitempty"`
}

// SourceUpdate marks a row a database source stored: UPDATE Table SET
// each Set column to its value WHERE Key = the row's id.
type SourceUpdate struct {
	Table string            `json:"table"`
	Key   string            `json:"key"`
	Set   map[string]string `json:"set"`
}

// Destination is one delivery target of a flow.
type Destination struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Dir  string `json:"dir,omitempty"`
	// Address is the host:port an mllp destination delivers to (#107 D-64).
	Address string `json:"address,omitempty"`
	// TLS makes an mllp destination connect over TLS, trusting the
	// system's roots or, when set, only the certificates in CAFile (#107
	// D-71).
	TLS    bool   `json:"tls,omitempty"`
	CAFile string `json:"caFile,omitempty"`
	// FrameStart, FrameEnd, and AckMode set an mllp destination's framing
	// and whether it waits for ACKs (#107 D-72).
	FrameStart string `json:"frameStart,omitempty"`
	FrameEnd   string `json:"frameEnd,omitempty"`
	AckMode    string `json:"ackMode,omitempty"`
	// Driver, DSNEnv, Table, Columns (column -> path in the message), and
	// KeyColumn describe a database destination's insert (#107 D-75); the
	// connection string is the server environment variable DSNEnv.
	Driver    string            `json:"driver,omitempty"`
	DSNEnv    string            `json:"dsnEnv,omitempty"`
	Table     string            `json:"table,omitempty"`
	Columns   map[string]string `json:"columns,omitempty"`
	KeyColumn string            `json:"keyColumn,omitempty"`
	// Flow is the id of the flow a flow destination hands messages to
	// (#107 D-70).
	Flow string `json:"flow,omitempty"`
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
// http or mllp source's address is host:port with a non-zero port.
func CheckSource(s *Source) error {
	if s == nil {
		return nil
	}
	if s.Type == "mllp" {
		if *s != (Source{Type: "mllp", Address: s.Address, CertFile: s.CertFile, KeyFile: s.KeyFile, FrameStart: s.FrameStart, FrameEnd: s.FrameEnd, AckMode: s.AckMode}) {
			return errors.New("an mllp source takes only type, address, certFile, keyFile, frameStart, frameEnd, and ackMode")
		}
		if err := checkMLLPMode(s.FrameStart, s.FrameEnd, s.AckMode); err != nil {
			return fmt.Errorf("source.%w", err)
		}
	} else if s.FrameStart != "" || s.FrameEnd != "" || s.AckMode != "" {
		return errors.New("source.frameStart, frameEnd, and ackMode apply only to mllp sources")
	}
	if err := checkSchedule(s); err != nil {
		return err
	}
	if s.Type == "database" {
		return checkDatabaseSource(s)
	}
	if s.Driver != "" || s.DSNEnv != "" || s.Query != "" || s.IDColumn != "" || s.Update != nil || s.MaxRows != 0 || s.TimeoutMs != 0 {
		return errors.New("source.driver, dsnEnv, query, idColumn, update, maxRows, and timeoutMs apply only to database sources")
	}
	if s.Listens() {
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

// Within reports whether path is dir or inside it, comparing both as
// written and with symbolic links resolved (a recursive source follows a
// linked dir, so /link/done and /real/done can be the same place).
func Within(path, dir string) bool {
	return inside(filepath.Clean(path), filepath.Clean(dir)) || inside(resolve(path), resolve(dir))
}

func inside(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolve is p with symbolic links resolved in its longest existing
// prefix (a directory may not exist yet: moveTo is created on first use).
func resolve(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
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
