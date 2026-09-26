// Package pipeline runs a received message through a flow: persist, filter and
// transform with the YAML DSL, then deliver to each destination through the
// outbox (spec §6.2, gap #5).
package pipeline

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/dsl"
	"github.com/weavster-dev/weavster/internal/outbox"
	"github.com/weavster-dev/weavster/internal/state"
)

// ErrInvalidMessage is returned when a message cannot be processed by the
// flow's transform (for example, the body is not a JSON object).
var ErrInvalidMessage = errors.New("pipeline: invalid message")

// Destination is one delivery target of a flow.
type Destination struct {
	Name string
	Type string // "http" or "file"
	URL  string // http
	Dir  string // file
}

// Flow is the processing definition the pipeline runs.
type Flow struct {
	ID           string
	Transform    *compiler.Transform // nil passes messages through unchanged
	Destinations []Destination
}

// Delivery is one message sent to one destination.
type Delivery struct {
	MessageID      string
	Body           []byte
	ContentType    string // MIME type of Body
	IdempotencyKey string // stable across retries (D-10)
}

// Sink delivers messages to a destination.
type Sink interface {
	Write(ctx context.Context, d Delivery) error
}

// SinkFactory builds the sink for a destination.
type SinkFactory func(Destination) (Sink, error)

// Result is the outcome of processing one message.
type Result struct {
	ID     string
	Status state.Status
}

// Observer is told when a message is received and when it finishes
// processing, with its final state (status, per-destination attempts, error
// metadata). A message that fails after it was received is reported as
// errored.
type Observer interface {
	Received(flowID string)
	Processed(m state.Message)
	// Retried reports a queued message after a retry pass, with the
	// destinations that were attempted.
	Retried(m state.Message, attempted []string)
}

// Options configures delivery retries.
type Options struct {
	MaxAttempts int           // attempts per destination before dead-lettering (default 5)
	BackoffBase time.Duration // first retry delay, doubled per attempt, capped at 1 minute (default 1s)
}

// ErrFlowGone is returned by a FlowLookup when the message's flow no longer
// exists; RetryDue dead-letters such messages.
var ErrFlowGone = errors.New("pipeline: flow no longer exists")

// FlowLookup returns the current definition of a flow.
type FlowLookup func(ctx context.Context, flowID string) (Flow, error)

// Pipeline processes messages against a Store.
type Pipeline struct {
	store    state.Store
	sinks    SinkFactory
	observer Observer
	opts     Options
}

// New returns a pipeline persisting to store and delivering through sinks.
// observer may be nil.
func New(store state.Store, sinks SinkFactory, observer Observer, opts Options) *Pipeline {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = time.Second
	}
	return &Pipeline{store: store, sinks: sinks, observer: observer, opts: opts}
}

func (p *Pipeline) outbox(f Flow, contentType string) *outbox.Outbox {
	return outbox.New(p.store, p.deliverFunc(f, contentType), outbox.Options{MaxAttempts: p.opts.MaxAttempts, BackoffBase: p.opts.BackoffBase})
}

// Validate checks a flow definition: the transform compiles and every
// destination is complete and uniquely named.
func Validate(f Flow) error {
	if f.Transform != nil {
		if _, err := dsl.Compile(*f.Transform); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, d := range f.Destinations {
		switch {
		case d.Name == "":
			return fmt.Errorf("destinations[%d]: name is required", i)
		case seen[d.Name]:
			return fmt.Errorf("destinations[%d]: duplicate name %q", i, d.Name)
		case d.Type == "http" && !validHTTPURL(d.URL):
			return fmt.Errorf("destination %s: url must be an absolute http:// or https:// URL, got %q", d.Name, d.URL)
		case d.Type == "file" && d.Dir == "":
			return fmt.Errorf("destination %s: dir is required for type file", d.Name)
		case d.Type != "http" && d.Type != "file":
			return fmt.Errorf("destination %s: type must be http or file, got %q", d.Name, d.Type)
		}
		seen[d.Name] = true
	}
	return nil
}

func validHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Process runs body through flow f and returns the stored message's id and
// aggregate status. Delivery failures do not return an error: they are
// recorded per destination and leave the message queued.
func (p *Pipeline) Process(ctx context.Context, f Flow, body []byte) (_ Result, err error) {
	var prog *dsl.Program
	var doc map[string]any
	if f.Transform != nil {
		var err error
		if prog, err = dsl.Compile(*f.Transform); err != nil {
			return Result{}, err
		}
		// UseNumber keeps numbers exact (e.g. 20-digit identifiers) in fields
		// the transform does not touch.
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(&doc); err != nil || doc == nil {
			return Result{}, fmt.Errorf("%w: body must be a JSON object", ErrInvalidMessage)
		}
		if _, err := dec.Token(); err != io.EOF {
			return Result{}, fmt.Errorf("%w: body must be a single JSON object", ErrInvalidMessage)
		}
	}

	id, err := newID()
	if err != nil {
		return Result{}, err
	}
	contentType := "raw"
	if prog != nil {
		contentType = "json"
	}
	m := state.Message{ID: id, FlowID: f.ID, ContentType: contentType, Raw: body, Original: body}
	ob := p.outbox(f, contentType)
	if err := ob.Receive(ctx, m); err != nil {
		return Result{}, err
	}
	if p.observer != nil {
		p.observer.Received(f.ID)
		defer func() {
			if err != nil { // stored but not finished: report it as errored
				p.observer.Processed(state.Message{ID: id, FlowID: f.ID, Status: state.StatusErrored})
			}
		}()
	}

	if prog != nil {
		out, filtered, err := prog.Run(doc)
		if err != nil {
			return p.finish(ctx, id, state.StatusErrored, err)
		}
		if filtered {
			return p.finish(ctx, id, state.StatusFiltered, nil)
		}
		transformed, err := json.Marshal(out)
		if err != nil {
			return p.finish(ctx, id, state.StatusErrored, err)
		}
		if err := ob.Transform(ctx, id, func([]byte) ([]byte, error) { return transformed, nil }); err != nil {
			return Result{}, err
		}
	} else if err := ob.Transform(ctx, id, func(raw []byte) ([]byte, error) { return raw, nil }); err != nil {
		return Result{}, err
	}

	for _, d := range f.Destinations {
		if err := ob.Deliver(ctx, id, d.Name); err != nil {
			return Result{}, err
		}
	}
	return p.aggregate(ctx, id, f)
}

// deliverFunc routes outbox deliveries to the flow's sinks, building each
// sink once per message.
func (p *Pipeline) deliverFunc(f Flow, contentType string) outbox.DeliverFunc {
	mime := "application/octet-stream"
	if contentType == "json" {
		mime = "application/json"
	}
	type built struct {
		sink Sink
		err  error
	}
	sinks := map[string]built{}
	for _, d := range f.Destinations {
		s, err := p.sinks(d)
		sinks[d.Name] = built{s, err}
	}
	return func(ctx context.Context, m state.Message, dest, key string) error {
		b := sinks[dest]
		if b.err != nil {
			return b.err
		}
		return b.sink.Write(ctx, Delivery{MessageID: m.ID, Body: m.Transformed, ContentType: mime, IdempotencyKey: key})
	}
}

// aggregate sets the message status from the per-destination results.
func (p *Pipeline) aggregate(ctx context.Context, id string, f Flow) (Result, error) {
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	return p.finish(ctx, id, p.rollup(m, f), nil)
}

// rollup is sent when every destination succeeded, dead-lettered when a
// destination exhausted its attempts, and otherwise queued.
func (p *Pipeline) rollup(m state.Message, f Flow) state.Status {
	status := state.StatusSent
	for _, d := range f.Destinations {
		a := m.Attempts[d.Name]
		switch {
		case a.Attempts > 0 && a.LastError == "":
		case a.Attempts >= p.opts.MaxAttempts:
			return state.StatusDeadLettered
		default:
			status = state.StatusQueued
		}
	}
	return status
}

// RetryDue re-delivers every queued message whose failed destinations are
// due, and returns how many messages it attempted. Deliveries reuse the
// message's idempotency keys. It is safe to call at startup to resume work
// left queued before a restart.
func (p *Pipeline) RetryDue(ctx context.Context, lookup FlowLookup) (int, error) {
	queued, err := p.store.Search(ctx, state.Query{Status: state.StatusQueued, Limit: 500})
	if err != nil {
		return 0, err
	}
	now := time.Now()
	n := 0
	for _, m := range queued {
		if !anyDue(m, now) {
			continue
		}
		n++
		f, err := lookup(ctx, m.FlowID)
		if errors.Is(err, ErrFlowGone) {
			if _, err := p.finishRetry(ctx, m.ID, state.StatusDeadLettered, ErrFlowGone, nil); err != nil {
				return n, err
			}
			continue
		}
		if err != nil {
			return n, err
		}
		ob := p.outbox(f, m.ContentType)
		var attempted []string
		for _, d := range f.Destinations {
			if a, ok := m.Attempts[d.Name]; ok && a.LastError != "" && !a.NextAttemptAt.After(now) {
				if err := ob.Deliver(ctx, m.ID, d.Name); err != nil {
					return n, err
				}
				attempted = append(attempted, d.Name)
			}
		}
		latest, err := p.store.Get(ctx, m.ID)
		if err != nil {
			return n, err
		}
		if _, err := p.finishRetry(ctx, m.ID, p.rollup(latest, f), nil, attempted); err != nil {
			return n, err
		}
	}
	return n, nil
}

func anyDue(m state.Message, now time.Time) bool {
	for _, a := range m.Attempts {
		if a.LastError != "" && !a.NextAttemptAt.After(now) {
			return true
		}
	}
	return false
}

// finishRetry stores a retry pass's status and reports it to the observer.
func (p *Pipeline) finishRetry(ctx context.Context, id string, status state.Status, cause error, attempted []string) (Result, error) {
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	m.Status = status
	if cause != nil {
		if m.Metadata == nil {
			m.Metadata = map[string]string{}
		}
		m.Metadata["error"] = cause.Error()
	}
	if err := p.store.Put(ctx, m); err != nil {
		return Result{}, err
	}
	if p.observer != nil {
		p.observer.Retried(m, attempted)
	}
	return Result{ID: id, Status: status}, nil
}

// finish records the aggregate status, and a processing error in the
// message's "error" metadata.
func (p *Pipeline) finish(ctx context.Context, id string, status state.Status, cause error) (Result, error) {
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	m.Status = status
	if cause != nil {
		if m.Metadata == nil {
			m.Metadata = map[string]string{}
		}
		m.Metadata["error"] = cause.Error()
	}
	if err := p.store.Put(ctx, m); err != nil {
		return Result{}, err
	}
	if p.observer != nil {
		p.observer.Processed(m)
	}
	return Result{ID: id, Status: status}, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
