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
	fail   error
}

func (s *recordingSink) Write(_ context.Context, _ string, body []byte, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.bodies = append(s.bodies, string(body))
	s.keys = append(s.keys, key)
	return nil
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
		{"ok", Flow{Destinations: []Destination{{Name: "a", Type: "http", URL: "http://x"}, {Name: "b", Type: "file", Dir: "/tmp/x"}}}, ""},
		{"bad transform", Flow{Transform: &compiler.Transform{Name: "t", Steps: []compiler.Step{{}}}}, "exactly one of"},
		{"no name", Flow{Destinations: []Destination{{Type: "http", URL: "u"}}}, "name is required"},
		{"duplicate", Flow{Destinations: []Destination{{Name: "a", Type: "file", Dir: "d"}, {Name: "a", Type: "file", Dir: "d"}}}, "duplicate name"},
		{"http without url", Flow{Destinations: []Destination{{Name: "a", Type: "http"}}}, "url is required"},
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
			p := New(store, func(d Destination) (Sink, error) {
				if d.Name == "a" {
					return a, nil
				}
				return b, nil
			})
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
	p := New(state.NewMemStore(), func(Destination) (Sink, error) { return nil, errors.New("no sink") })
	f := Flow{ID: "f", Transform: transform(t, "name: t\nsteps:\n  - set: { field: a, expr: b }")}
	for _, body := range []string{`not json`, `null`, `[1]`} {
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
				_, err := New(store, sink).Process(ctx, f, []byte(`{}`))
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
