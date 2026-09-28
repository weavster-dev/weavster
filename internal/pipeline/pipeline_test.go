package pipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/state"
)

type recordingSink struct {
	mu     sync.Mutex
	bodies []string
	keys   []string
	types  []string
	fail   error
}

func (s *recordingSink) Write(_ context.Context, d Delivery) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.bodies = append(s.bodies, string(d.Body))
	s.keys = append(s.keys, d.IdempotencyKey)
	s.types = append(s.types, d.ContentType)
	return nil
}

type recordingObserver struct {
	received int
	seen     []state.Message
	retried  []state.Message
}

func (o *recordingObserver) Received(string)           { o.received++ }
func (o *recordingObserver) Processed(m state.Message) { o.seen = append(o.seen, m) }
func (o *recordingObserver) Retried(m state.Message, _ []string) {
	o.retried = append(o.retried, m)
}

func transform(t *testing.T, yaml string) *compiler.Transform {
	t.Helper()
	tr, err := compiler.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		flow Flow
		want string
	}{
		{"ok", Flow{Destinations: []Destination{{Name: "a", Type: "http", URL: "https://x.example"}, {Name: "b", Type: "file", Dir: "/tmp/x"}}}, ""},
		{"bad transform", Flow{Transform: &compiler.Transform{Name: "t", Steps: []compiler.Step{{}}}}, "exactly one of"},
		{"no name", Flow{Destinations: []Destination{{Type: "http", URL: "u"}}}, "name is required"},
		{"duplicate", Flow{Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "a", Type: "file", Dir: "d"}}}, "duplicate name"},
		{"http without url", Flow{Destinations: []Destination{{Name: "a", Type: "http"}}}, "absolute http:// or https:// URL"},
		{"http without scheme", Flow{Destinations: []Destination{{Name: "a", Type: "http", URL: "ehr.example.com/in"}}}, "absolute http:// or https:// URL"},
		{"ftp url", Flow{Destinations: []Destination{{Name: "a", Type: "http", URL: "ftp://x"}}}, "absolute http:// or https:// URL"},
		{"file without dir", Flow{Destinations: []Destination{{Name: "a", Type: "file"}}}, "dir is required"},
		{"bad type", Flow{Destinations: []Destination{{Name: "a", Type: "smtp"}}}, "type must be http, file, or mllp"},
		{"mllp", Flow{InputFormat: "hl7v2", Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575"}}}, ""},
		{"mllp from json", Flow{Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575"}}}, "needs an HL7 v2 message"},
		{"mllp after a transform", Flow{InputFormat: "hl7v2", Transform: &compiler.Transform{Name: "t"}, Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575"}}}, "needs an HL7 v2 message"},
		{"mllp with its own transform", Flow{InputFormat: "hl7v2", Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575", Transform: &compiler.Transform{Name: "t"}}}}, "needs an HL7 v2 message"},
		{"mllp after an hl7v2 build", Flow{Transform: buildTo("hl7v2"), Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575"}}}, ""},
		{"mllp with its own hl7v2 build", Flow{Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575", Transform: buildTo("hl7v2")}}}, ""},
		{"mllp after an xml build", Flow{Transform: buildTo("xml"), Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example:2575"}}}, "needs an HL7 v2 message"},
		{"build in a response transform", Flow{ResponseSelector: "a", Destinations: []Destination{{Name: "a", Type: "http", URL: "https://x", ResponseTransform: buildTo("text")}}}, "build cannot be used here"},
		{"transform after a text build", Flow{Transform: buildTo("text"), Destinations: []Destination{{Name: "a", Type: "file", Dir: "d", Transform: &compiler.Transform{Name: "t"}}}}, "outputs text, which a transform cannot read"},
		{"mllp without port", Flow{Destinations: []Destination{{Name: "a", Type: "mllp", Address: "lab.example"}}}, "address must be host:port"},
		{"mllp without host", Flow{Destinations: []Destination{{Name: "a", Type: "mllp", Address: ":2575"}}}, "address must be host:port"},
		{"mllp port zero", Flow{Destinations: []Destination{{Name: "a", Type: "mllp", Address: "x:0"}}}, "address must be host:port"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.flow)
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestProcess(t *testing.T) {
	const normalize = `name: n
steps:
  - map: { from: "name.last", to: "patient.lastName" }
  - filter: { when: "patient.lastName == ''", action: reject }
  - set: { field: "patient.label", expr: "Patient {{patient.lastName}}" }
  - map: { from: "count", to: "n", type: number }`

	tests := []struct {
		name       string
		transform  string
		body       string
		failSinkB  error
		wantStatus state.Status
		wantBody   string // body delivered to both sinks; "" = nothing delivered
		wantError  string // "error" metadata
	}{
		{name: "passthrough", body: `raw bytes`, wantStatus: state.StatusSent, wantBody: `raw bytes`},
		{name: "transformed", transform: normalize, body: `{"name":{"last":"Doe"}}`, wantStatus: state.StatusSent,
			wantBody: `{"name":{"last":"Doe"},"patient":{"label":"Patient Doe","lastName":"Doe"}}`},
		{name: "filtered", transform: normalize, body: `{"name":{}}`, wantStatus: state.StatusFiltered},
		{name: "transform error", transform: normalize, body: `{"name":{"last":"Doe"},"count":"x"}`, wantStatus: state.StatusErrored, wantError: "is not a number"},
		{name: "one destination fails", body: `x`, failSinkB: errors.New("connection refused"), wantStatus: state.StatusQueued, wantBody: "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			store := state.NewMemStore()
			a, b := &recordingSink{}, &recordingSink{fail: tt.failSinkB}
			obs := &recordingObserver{}
			p := New(store, func(d Destination) (Sink, error) {
				if d.Name == "a" {
					return a, nil
				}
				return b, nil
			}, obs, Options{})
			f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x"}, {Name: "b", Type: "file", Dir: "y"}}}
			if tt.transform != "" {
				f.Transform = transform(t, tt.transform)
			}
			res, err := p.Process(ctx, f, []byte(tt.body))
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != tt.wantStatus {
				t.Errorf("status = %s, want %s", res.Status, tt.wantStatus)
			}
			m, err := store.Get(ctx, res.ID)
			if err != nil {
				t.Fatal(err)
			}
			if m.Status != tt.wantStatus || m.FlowID != "f" || string(m.Raw) != tt.body {
				t.Errorf("stored = %+v", m)
			}
			if obs.received != 1 || len(obs.seen) != 1 || obs.seen[0].ID != res.ID || obs.seen[0].Status != tt.wantStatus {
				t.Errorf("observer saw %+v, want the final message", obs.seen)
			}
			if tt.wantError != "" && !strings.Contains(m.Metadata["error"], tt.wantError) {
				t.Errorf("error metadata = %q, want %q", m.Metadata["error"], tt.wantError)
			}
			if tt.wantBody == "" {
				if len(a.bodies)+len(b.bodies) != 0 {
					t.Errorf("delivered %v %v, want nothing", a.bodies, b.bodies)
				}
				return
			}
			if len(a.bodies) != 1 || a.bodies[0] != tt.wantBody {
				t.Errorf("sink a got %v, want %q", a.bodies, tt.wantBody)
			}
			if tt.failSinkB == nil && (len(b.bodies) != 1 || b.bodies[0] != tt.wantBody) {
				t.Errorf("sink b got %v", b.bodies)
			}
			if tt.failSinkB != nil && m.Attempts["b"].LastError != tt.failSinkB.Error() {
				t.Errorf("attempts[b] = %+v", m.Attempts["b"])
			}
			if len(a.keys) != 1 || len(a.keys[0]) != 64 {
				t.Errorf("idempotency key = %v", a.keys)
			}
		})
	}
}

func TestProcessErrors(t *testing.T) {
	ctx := context.Background()
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return nil, errors.New("no sink") }, nil, Options{})
	f := Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: a, expr: b }")}
	for _, body := range []string{`not json`, `null`, `[1]`, `{} trailing`, `{}{}`} {
		if _, err := p.Process(ctx, f, []byte(body)); !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("body %s: err = %v, want ErrInvalidMessage", body, err)
		}
	}
	bad := Flow{ID: "f", Transform: &compiler.Transform{Name: "t", Steps: []compiler.Step{{}}}}
	if _, err := p.Process(ctx, bad, []byte(`{}`)); err == nil {
		t.Error("invalid transform: want error")
	}

	// A sink that cannot be built leaves the message queued.
	withDest := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	res, err := p.Process(ctx, withDest, []byte(`x`))
	if err != nil || res.Status != state.StatusQueued {
		t.Errorf("res = %+v, err = %v; want queued", res, err)
	}
}

// flakyStore fails the Nth Put or Get (1-based; 0 = never).
type flakyStore struct {
	*state.MemStore
	puts, gets       int
	failPut, failGet int
}

var errStore = errors.New("store down")

func (s *flakyStore) Put(ctx context.Context, m state.Message) error {
	s.puts++
	if s.puts == s.failPut {
		return errStore
	}
	return s.MemStore.Put(ctx, m)
}

func (s *flakyStore) Get(ctx context.Context, id string) (state.Message, error) {
	s.gets++
	if s.gets == s.failGet {
		return state.Message{}, errStore
	}
	return s.MemStore.Get(ctx, id)
}

// TestProcessStoreFailures fails each persistence call in turn and checks
// Process reports the error.
func TestProcessStoreFailures(t *testing.T) {
	ctx := context.Background()
	sink := func(Destination) (Sink, error) { return &recordingSink{}, nil }
	flows := map[string]Flow{
		"passthrough": {ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}},
		"transform":   {ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: a, expr: b }")},
		"filtered":    {ID: "f", Transform: transform(t, "name: t\nsteps:\n  - filter: { when: a, action: accept }")},
	}
	for name, f := range flows {
		for n := 1; n <= 6; n++ {
			for _, failGet := range []bool{false, true} {
				store := &flakyStore{MemStore: state.NewMemStore()}
				if failGet {
					store.failGet = n
				} else {
					store.failPut = n
				}
				obs := &recordingObserver{}
				_, err := New(store, sink, obs, Options{}).Process(ctx, f, []byte(`{}`))
				if err != nil && obs.received == 1 && (len(obs.seen) == 0 || obs.seen[len(obs.seen)-1].Status != state.StatusErrored) {
					t.Errorf("%s: a received message that failed was not reported as errored: %+v", name, obs.seen)
				}
				calls := store.puts
				if failGet {
					calls = store.gets
				}
				if calls >= n && !errors.Is(err, errStore) {
					t.Errorf("%s: failing call %d (get=%v): err = %v, want store error", name, n, failGet, err)
				}
			}
		}
	}
}

func TestProcessKeepsNumbersAndContentTypes(t *testing.T) {
	ctx := context.Background()
	sink := &recordingSink{}
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return sink, nil }, nil, Options{})
	f := Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - filter: { when: \"mrn == 12345678901234567890\", action: accept }\n  - set: { field: label, expr: '{{mrn}}' }"),
		Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	res, err := p.Process(ctx, f, []byte(`{"mrn":12345678901234567890}`))
	if err != nil || res.Status != state.StatusSent {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if sink.bodies[0] != `{"label":"12345678901234567890","mrn":12345678901234567890}` || sink.types[0] != "application/json" {
		t.Errorf("delivered %s as %s; want exact digits as application/json", sink.bodies[0], sink.types[0])
	}
	pass := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	if _, err := p.Process(ctx, pass, []byte("MSH|^~\\&|")); err != nil {
		t.Fatal(err)
	}
	if sink.types[1] != "application/octet-stream" {
		t.Errorf("passthrough content type = %s", sink.types[1])
	}
}

func TestProcessWithMetadataAndRemove(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	p := New(store, func(Destination) (Sink, error) { return &recordingSink{}, nil }, nil, Options{})
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x"}}}
	res, err := p.ProcessWithMetadata(ctx, f, []byte("x"), map[string]string{"reprocessedFrom": "m0"})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Metadata["reprocessedFrom"] != "m0" {
		t.Errorf("metadata = %v", m.Metadata)
	}
	p.inflight.Store(res.ID, struct{}{})
	if err := p.Remove(ctx, res.ID); !errors.Is(err, ErrInFlight) {
		t.Errorf("Remove of an in-flight message = %v, want ErrInFlight", err)
	}
	p.inflight.Delete(res.ID)
	if err := p.Remove(ctx, res.ID); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(ctx, res.ID); !errors.Is(err, state.ErrNotFound) {
		t.Errorf("Remove of a missing message = %v, want state.ErrNotFound", err)
	}
}

func TestHold(t *testing.T) {
	p := New(state.NewMemStore(), nil, nil, Options{})
	release, ok := p.Hold("m")
	if !ok {
		t.Fatal("first hold refused")
	}
	if _, again := p.Hold("m"); again {
		t.Error("second hold of a held id granted")
	}
	release()
	if release2, ok := p.Hold("m"); !ok {
		t.Error("hold after release refused")
	} else {
		release2()
	}
}

// TestProcessReturnsIDOnceStored: a failure after the message was stored
// still returns its id, so a caller never sends the same content again.
func TestProcessReturnsIDOnceStored(t *testing.T) {
	ctx := context.Background()
	f := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	for _, tt := range []struct {
		failPut int
		stored  bool
	}{
		{1, false}, // the first write (receive) fails: nothing stored
		{2, true},  // a later write fails: the message exists
	} {
		store := &flakyStore{MemStore: state.NewMemStore(), failPut: tt.failPut}
		p := New(store, func(Destination) (Sink, error) { return &toggleSink{}, nil }, nil, Options{})
		res, err := p.Process(ctx, f, []byte("x"))
		if !errors.Is(err, errStore) || (res.ID != "") != tt.stored {
			t.Errorf("failPut %d: id %q, err %v", tt.failPut, res.ID, err)
		}
	}
}

// TestProcessHL7Input: with inputFormat hl7v2, transforms read the HL7 v2
// message's JSON view (the flow's, or a destination's when the flow has
// none); a message that is not HL7 is refused.
func TestProcessHL7Input(t *testing.T) {
	ctx := context.Background()
	sink := &recordingSink{}
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return sink, nil }, nil, Options{})
	msg := []byte("MSH|^~\\&|LAB|HOSP|W|H|1||ADT^A01|C1|P|2.5\rPID|1||123||DOE^JOHN\r")
	flowT := Flow{ID: "f", InputFormat: "hl7v2", Transform: transform(t, "name: t\nsteps:\n  - map: { from: PID.5.1, to: last }"),
		Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	destT := Flow{ID: "g", InputFormat: "hl7v2", Destinations: []Destination{
		{Name: "a", Type: "file", Dir: "d", Transform: transform(t, "name: d\nsteps:\n  - filter: { when: \"MSH.9.2 == 'A01'\", action: accept }\n  - map: { from: PID.5.2, to: first }")},
	}}
	for i, f := range []Flow{flowT, destT} {
		res, err := p.Process(ctx, f, msg)
		if err != nil || res.Status != state.StatusSent {
			t.Fatalf("%s: res = %+v, err = %v", f.ID, res, err)
		}
		want := []string{`"last":"DOE"`, `"first":"JOHN"`}[i]
		if !strings.Contains(sink.bodies[i], want) || sink.types[i] != "application/json" {
			t.Errorf("%s: delivered %s as %s", f.ID, sink.bodies[i], sink.types[i])
		}
	}
	pass := Flow{ID: "h", InputFormat: "hl7v2", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	for _, f := range []Flow{flowT, pass} {
		_, err := p.Process(ctx, f, []byte(`{"PID":{}}`))
		var invalid *InvalidMessageError
		if !errors.As(err, &invalid) || invalid.Reason != "body must be an HL7 v2 message (MSH segment first)" || !errors.Is(err, ErrInvalidMessage) {
			t.Errorf("%s: JSON into an hl7v2 flow: %v", f.ID, err)
		}
	}

	// A message stored as received (no flow transform then) is read as HL7
	// by destination transforms even after a flow transform is added.
	later := destT
	later.Transform = transform(t, "name: t\nsteps:\n  - set: { field: x, expr: y }")
	outs := destinationOutputs(later, state.Message{ContentType: "raw", Transformed: msg})
	if r := outs["a"]; r.err != nil || !strings.Contains(string(r.body), `"first":"JOHN"`) {
		t.Errorf("stored HL7 after the definition changed: %s, %v", r.body, r.err)
	}
}

// TestProcessXMLInput: with inputFormat xml, transforms read the XML
// document's JSON view; anything else is refused with a fixed reason.
func TestProcessXMLInput(t *testing.T) {
	ctx := context.Background()
	sink := &recordingSink{}
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return sink, nil }, nil, Options{})
	f := Flow{ID: "x", InputFormat: "xml", Transform: transform(t, "name: t\nsteps:\n  - map: { from: order.@id, to: id }\n  - map: { from: order.patient.name.#text, to: name }"),
		Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	if _, err := p.Process(ctx, f, []byte(`<order id="7"><patient><name>DOE</name></patient></order>`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.bodies[0], `"id":"7"`) || !strings.Contains(sink.bodies[0], `"name":"DOE"`) {
		t.Errorf("delivered %s", sink.bodies[0])
	}
	pass := Flow{ID: "y", InputFormat: "xml", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}}}
	for body, want := range map[string]string{
		`{"a":1}`:     "body must be a single well-formed XML document: text outside the root element",
		`<a/><b/>`:    "body must be a single well-formed XML document: more than one root element",
		`<a><secret>`: "body must be a single well-formed XML document",
	} {
		_, err := p.Process(ctx, pass, []byte(body))
		var invalid *InvalidMessageError
		if !errors.As(err, &invalid) || invalid.Reason != want {
			t.Errorf("%s: %v, want %q", body, err, want)
		}
	}
}

// TestProcessDelimitedInput: with inputFormat delimited, transforms read
// {"rows": ...}, with or without a header row and with any delimiter.
func TestProcessDelimitedInput(t *testing.T) {
	ctx := context.Background()
	sink := &recordingSink{}
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return sink, nil }, nil, Options{})
	dest := []Destination{{Name: "a", Type: "file", Dir: "d"}}
	header := Flow{ID: "h", InputFormat: "delimited", Transform: transform(t, "name: t\nsteps:\n  - map: { from: rows.1.last, to: second }"), Destinations: dest}
	tabs := Flow{ID: "t", InputFormat: "delimited", Delimiter: '\t', NoHeader: true, Transform: transform(t, "name: t\nsteps:\n  - map: { from: rows.0.1, to: last }"), Destinations: dest}
	if _, err := p.Process(ctx, header, []byte("mrn,last\n1,DOE\n2,ROE\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Process(ctx, tabs, []byte("1\tDOE\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sink.bodies[0], `"second":"ROE"`) || !strings.Contains(sink.bodies[1], `"last":"DOE"`) {
		t.Errorf("delivered %v", sink.bodies)
	}
	for body, want := range map[string]string{
		"a,b\n1\n":  "body must be valid delimited text: rows have different numbers of fields",
		"a\n\"x\"y": `body must be valid delimited text: a quoted value is not closed, or has a " not doubled`,
		"":          "body must be valid delimited text: no rows",
	} {
		_, err := p.Process(ctx, header, []byte(body))
		var invalid *InvalidMessageError
		if !errors.As(err, &invalid) || invalid.Reason != want {
			t.Errorf("%q: %v, want %q", body, err, want)
		}
	}
}

// TestProcessDestinationSet: the flow's destinationSet steps exclude
// destinations for a message, the exclusion is stored with the message and
// honored by retries, and a message with every destination excluded is
// filtered.
func TestProcessDestinationSet(t *testing.T) {
	ctx := context.Background()
	sinks := map[string]*recordingSink{"ehr": {}, "archive": {}}
	store := state.NewMemStore()
	p := New(store, func(d Destination) (Sink, error) { return sinks[d.Name], nil }, nil, Options{MaxAttempts: 3})
	f := Flow{ID: "f",
		Transform:    transform(t, "name: t\nsteps:\n  - destinationSet: { exclude: [ehr], when: \"kind == 'orm'\" }\n  - destinationSet: { exclude: [archive], when: test }"),
		Destinations: []Destination{{Name: "ehr", Type: "file", Dir: "d"}, {Name: "archive", Type: "file", Dir: "d"}}}
	for _, tt := range []struct {
		body, status, ehr, archive, excluded string
	}{
		{`{"kind":"adt"}`, "sent", "1", "1", ""},
		{`{"kind":"orm"}`, "sent", "1", "2", "ehr"},
		{`{"kind":"orm","test":true}`, "filtered", "1", "2", "archive,ehr"},
	} {
		res, err := p.Process(ctx, f, []byte(tt.body))
		if err != nil || string(res.Status) != tt.status {
			t.Fatalf("%s: %+v %v", tt.body, res, err)
		}
		m, _ := store.Get(ctx, res.ID)
		if got := fmt.Sprint(len(sinks["ehr"].bodies), len(sinks["archive"].bodies)); got != tt.ehr+" "+tt.archive || m.Metadata[ExcludedMetadata] != tt.excluded {
			t.Errorf("%s: deliveries ehr/archive %s, excluded %q; want %s %s, %q", tt.body, got, m.Metadata[ExcludedMetadata], tt.ehr, tt.archive, tt.excluded)
		}
	}

	// A reprocessed message arrives with the old exclusion in its metadata;
	// this run excludes nothing, so it is removed and ehr gets the message.
	// The same holds for a flow with no transform at all.
	plain := Flow{ID: "p", Destinations: f.Destinations}
	res0, err := p.ProcessWithMetadata(ctx, plain, []byte(`{}`), map[string]string{ExcludedMetadata: "ehr"})
	if err != nil || res0.Status != state.StatusSent {
		t.Fatalf("reprocessed, no transform: %+v %v", res0, err)
	}
	if m, _ := store.Get(ctx, res0.ID); m.Metadata[ExcludedMetadata] != "" || len(sinks["ehr"].bodies) != 2 {
		t.Errorf("no transform: excluded %q, ehr deliveries %d; want none, 2", m.Metadata[ExcludedMetadata], len(sinks["ehr"].bodies))
	}
	res, err := p.ProcessWithMetadata(ctx, f, []byte(`{"kind":"adt"}`), map[string]string{ExcludedMetadata: "ehr"})
	if err != nil || res.Status != state.StatusSent {
		t.Fatalf("reprocessed: %+v %v", res, err)
	}
	if m, _ := store.Get(ctx, res.ID); m.Metadata[ExcludedMetadata] != "" || len(sinks["ehr"].bodies) != 3 {
		t.Errorf("reprocessed: excluded %q, ehr deliveries %d; want none, 3", m.Metadata[ExcludedMetadata], len(sinks["ehr"].bodies))
	}

	// A retry honors the stored exclusion even though archive failed first.
	sinks["archive"].fail = errors.New("down")
	res, err = p.Process(ctx, f, []byte(`{"kind":"orm"}`))
	if err != nil || res.Status != state.StatusQueued {
		t.Fatalf("with archive down: %+v %v", res, err)
	}
	sinks["archive"].fail = nil
	m, _ := store.Get(ctx, res.ID)
	m.Attempts["archive"] = state.DestinationAttempt{Attempts: 1, LastError: "down"} // due now
	_ = store.Put(ctx, m)
	if _, err := p.RetryDue(ctx, func(context.Context, string) (Flow, error) { return f, nil }); err != nil {
		t.Fatal(err)
	}
	if m, _ = store.Get(ctx, res.ID); m.Status != state.StatusSent || len(sinks["ehr"].bodies) != 3 {
		t.Errorf("after retry: status %s, ehr deliveries %d (want sent, 3: ehr stays excluded)", m.Status, len(sinks["ehr"].bodies))
	}
}

// buildTo is a transform whose only step builds format.
func buildTo(format string) *compiler.Transform {
	return &compiler.Transform{Name: "b", Steps: []compiler.Step{{Build: &compiler.BuildStep{Template: "MSH|^~\\&|A", Format: format}}}}
}

// TestProcessBuild: a flow's build step makes the output (with its
// Content-Type); a destination transform reads that output's view; a
// destination can build its own format.
func TestProcessBuild(t *testing.T) {
	ctx := context.Background()
	sinks := map[string]*recordingSink{"lab": {}, "ehr": {}, "raw": {}}
	store := state.NewMemStore()
	p := New(store, func(d Destination) (Sink, error) { return sinks[d.Name], nil }, nil, Options{})
	f := Flow{ID: "f", InputFormat: "hl7v2",
		Transform: transform(t, "name: t\nsteps:\n  - map: { from: PID.5.1, to: last }\n  - build: { format: hl7v2, template: \"MSH|^~\\\\&|W|H|LAB|H|1||ADT^A08|{{MSH.10.1}}|P|2.5\\nPID|1||||{{last}}\" }"),
		Destinations: []Destination{
			{Name: "lab", Type: "file", Dir: "d"},
			{Name: "ehr", Type: "file", Dir: "d", Transform: transform(t, "name: d\nsteps:\n  - build: { format: xml, template: \"<p last='{{PID.5.1}}' type='{{MSH.9.2}}'/>\" }")},
			{Name: "raw", Type: "file", Dir: "d", Transform: transform(t, "name: j\nsteps:\n  - map: { from: PID.5.1, to: name }")},
		}}
	res, err := p.Process(ctx, f, []byte("MSH|^~\\&|A|B|C|D|1||ADT^A01|C7|P|2.5\rPID|1||123||DOE^JOHN\r"))
	if err != nil || res.Status != state.StatusSent {
		t.Fatalf("%+v %v", res, err)
	}
	for name, want := range map[string]string{
		"lab": "x-application/hl7-v2+er7 MSH|^~\\&|W|H|LAB|H|1||ADT^A08|C7|P|2.5\rPID|1||||DOE\r",
		"ehr": "application/xml <p last='DOE' type='A08'/>",
		"raw": `application/json {"MSH":`,
	} {
		if got := sinks[name].types[0] + " " + sinks[name].bodies[0]; !strings.HasPrefix(got, want) {
			t.Errorf("%s got %q, want %q", name, got, want)
		}
	}
	if m, _ := store.Get(ctx, res.ID); m.ContentType != "hl7v2" {
		t.Errorf("stored content type %q", m.ContentType)
	}
	// A build that cannot produce its format errors the message.
	bad := Flow{ID: "b", Transform: transform(t, "name: t\nsteps:\n  - build: { format: xml, template: '<a>{{x}}' }"), Destinations: []Destination{{Name: "lab", Type: "file", Dir: "d"}}}
	if res, err := p.Process(ctx, bad, []byte(`{"x":1}`)); err != nil || res.Status != state.StatusErrored {
		t.Errorf("broken build: %+v %v", res, err)
	}
	for format, want := range map[string]string{"text": "text/plain; charset=utf-8", "raw": "application/octet-stream"} {
		if got := MimeType(format); got != want {
			t.Errorf("MimeType(%s) = %s", format, got)
		}
	}
}

// TestStoredTextOutput: a stored text output stays unreadable for
// destination transforms even after the flow definition changed.
func TestStoredTextOutput(t *testing.T) {
	later := Flow{ID: "f", Destinations: []Destination{{Name: "a", Type: "file", Dir: "d", Transform: transform(t, "name: d\nsteps:\n  - set: { field: x, expr: y }")}}}
	outs := destinationOutputs(later, state.Message{ContentType: "text", Transformed: []byte(`{"looks":"like json"}`)})
	if r := outs["a"]; r.err == nil || !strings.Contains(r.err.Error(), "output is text") {
		t.Errorf("text output read by a destination transform: %+v", r)
	}
}
