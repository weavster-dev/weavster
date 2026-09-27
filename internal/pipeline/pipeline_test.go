package pipeline

import (
	"context"
	"errors"
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
		{"bad type", Flow{Destinations: []Destination{{Name: "a", Type: "smtp"}}}, "type must be http or file"},
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
