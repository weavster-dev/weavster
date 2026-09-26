package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/state"
)

// replySink returns reply for every successful delivery.
type replySink struct {
	reply, contentType string
	fail               error
}

func (s replySink) Write(context.Context, Delivery) error { return s.fail }

func (s replySink) WriteResponse(context.Context, Delivery) (*Reply, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	ct := s.contentType
	if ct == "" {
		ct = "application/json"
	}
	return &Reply{Body: []byte(s.reply), ContentType: ct}, nil
}

func TestResponseSelector(t *testing.T) {
	tests := []struct {
		name     string
		selector string
		reply    string
		ctype    string
		fail     error
		rt       *compiler.Transform // response transform of "a"
		filter   *compiler.Transform // transform of "a"
		want     string
	}{
		{"no selector", "", `{"ok":true}`, "", nil, nil, nil, ``},
		{"json reply", "a", `{"ok":true}`, "", nil, nil, nil, `{"ok":true}`},
		{"json with parameters", "a", `[1]`, "application/fhir+json; charset=utf-8", nil, nil, nil, `[1]`},
		{"text reply is a string", "a", `MSA|AA`, "text/plain", nil, nil, nil, `"MSA|AA"`},
		{"text that parses as json stays a string", "a", `123`, "text/plain", nil, nil, nil, `"123"`},
		{"invalid json is a string", "a", `{`, "", nil, nil, nil, `"{"`},
		{"empty reply", "a", ``, "", nil, nil, nil, ``},
		{"response transform", "a", `{"ok":true}`, "", nil, steps(setField("ack", "yes")), nil, `{"ack":"yes","ok":true}`},
		{"response transform filters", "a", `{"ok":true}`, "", nil, steps(rejectWhen("ok")), nil, ``},
		{"response transform needs an object", "a", `MSA|AA`, "text/plain", nil, steps(setField("ack", "yes")), nil, ``},
		{"failed delivery", "a", `{"ok":true}`, "", errors.New("down"), nil, nil, ``},
		{"destination filtered the message", "a", `{"ok":true}`, "", nil, nil, steps(rejectWhen("kind")), ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(state.NewMemStore(), func(d Destination) (Sink, error) {
				if d.Name == "b" {
					return replySink{reply: `{"b":1}`}, nil
				}
				return replySink{reply: tt.reply, contentType: tt.ctype, fail: tt.fail}, nil
			}, nil, Options{})
			f := Flow{ID: "f", ResponseSelector: tt.selector, Destinations: []Destination{
				{Name: "a", Type: "http", URL: "http://a.example", Transform: tt.filter, ResponseTransform: tt.rt},
				{Name: "b", Type: "http", URL: "http://b.example"},
			}}
			if err := Validate(f); err != nil {
				t.Fatal(err)
			}
			res, err := p.Process(context.Background(), f, []byte(`{"kind":"adt"}`))
			if err != nil {
				t.Fatal(err)
			}
			if string(res.Response) != tt.want {
				t.Errorf("Response = %s, want %s", res.Response, tt.want)
			}
		})
	}
}

func TestValidateResponse(t *testing.T) {
	tests := []struct {
		name string
		f    Flow
		want string
	}{
		{"unknown selector", Flow{ResponseSelector: "zz", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x"}}}, `no destination named "zz"`},
		{"file selector", Flow{ResponseSelector: "a", Destinations: []Destination{{Name: "a", Type: "file", Dir: "x"}}}, "sends no reply"},
		{"bad response transform", Flow{ResponseSelector: "a", Destinations: []Destination{{Name: "a", Type: "http", URL: "http://a.example", ResponseTransform: steps(compiler.Step{})}}}, "destination a: responseTransform"},
		{"response transform not selected", Flow{Destinations: []Destination{{Name: "a", Type: "http", URL: "http://a.example", ResponseTransform: steps(setField("x", "y"))}}}, "only used on the responseSelector"},
	}
	for _, tt := range tests {
		if err := Validate(tt.f); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Validate = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// TestRetryReturnsNoResponse: a background retry has no sender to reply to.
func TestRetryReturnsNoResponse(t *testing.T) {
	ctx := context.Background()
	store := state.NewMemStore()
	fail := true
	p := New(store, func(Destination) (Sink, error) {
		if fail {
			return replySink{fail: errors.New("down")}, nil
		}
		return replySink{reply: `{"ok":true}`}, nil
	}, nil, Options{BackoffBase: 1})
	f := Flow{ID: "f", ResponseSelector: "a", Destinations: []Destination{{Name: "a", Type: "http", URL: "http://a.example"}}}
	res, err := p.Process(ctx, f, []byte(`{}`))
	if err != nil || res.Status != state.StatusQueued || res.Response != nil {
		t.Fatalf("Process = %+v, %v", res, err)
	}
	fail = false
	m, _ := store.Get(ctx, res.ID)
	retried, err := p.resume(ctx, f, m.ID, true)
	if err != nil || retried.Status != state.StatusSent || retried.Response != nil {
		t.Errorf("retry = %+v, %v; want sent with no response", retried, err)
	}
}
