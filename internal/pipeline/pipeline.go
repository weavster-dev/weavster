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
	"sync"
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
	Paused       bool                // stopped, paused, halted, or undeployed: RetryDue skips its messages
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
	// Gate, when set, is held while RetryDue works on a message of a flow,
	// so lifecycle changes and deletes of that flow are ordered after it.
	Gate interface {
		ProcessFlow(flowID string) (done func())
	}
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
	inflight sync.Map // ids of messages being processed or retried
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
// recorded per destination and leave the message queued for RetryDue.
func (p *Pipeline) Process(ctx context.Context, f Flow, body []byte) (_ Result, err error) {
	if f.Transform != nil {
		if _, err := dsl.Compile(*f.Transform); err != nil {
			return Result{}, err
		}
		if _, err := decodeObject(body); err != nil {
			return Result{}, err
		}
	}

	id, err := newID()
	if err != nil {
		return Result{}, err
	}
	// Mark the message in flight so RetryDue never works on it concurrently.
	p.inflight.Store(id, struct{}{})
	defer p.inflight.Delete(id)

	contentType := "raw"
	if f.Transform != nil {
		contentType = "json"
	}
	m := state.Message{ID: id, FlowID: f.ID, ContentType: contentType, Raw: body, Original: body}
	if err := p.outbox(f, contentType).Receive(ctx, m); err != nil {
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
	return p.resume(ctx, f, id, false)
}

// decodeObject decodes body as a single JSON object, keeping numbers exact
// (json.Number) so identifiers like 20-digit MRNs survive untouched fields.
func decodeObject(body []byte) (map[string]any, error) {
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, fmt.Errorf("%w: body must be a JSON object", ErrInvalidMessage)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: body must be a single JSON object", ErrInvalidMessage)
	}
	return doc, nil
}

// resume continues a stored message from where it stopped: it transforms a
// message still in "received", then delivers every destination that has not
// succeeded, is due, and has attempts left, and finally stores the rolled-up
// status. Process and RetryDue both use it, so a message interrupted at any
// point (including by a crash) is finished the same way.
func (p *Pipeline) resume(ctx context.Context, f Flow, id string, retry bool) (Result, error) {
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	ob := p.outbox(f, m.ContentType)
	if m.Status == state.StatusReceived {
		transformed := m.Raw
		if f.Transform != nil {
			prog, err := dsl.Compile(*f.Transform)
			if err != nil {
				return p.complete(ctx, id, state.StatusErrored, err, retry, nil)
			}
			doc, err := decodeObject(m.Raw)
			if err != nil {
				return p.complete(ctx, id, state.StatusErrored, err, retry, nil)
			}
			out, filtered, err := prog.Run(doc)
			if err != nil {
				return p.complete(ctx, id, state.StatusErrored, err, retry, nil)
			}
			if filtered {
				return p.complete(ctx, id, state.StatusFiltered, nil, retry, nil)
			}
			if transformed, err = json.Marshal(out); err != nil {
				return p.complete(ctx, id, state.StatusErrored, err, retry, nil)
			}
		}
		if err := ob.Transform(ctx, id, func([]byte) ([]byte, error) { return transformed, nil }); err != nil {
			return Result{}, err
		}
	}

	now := time.Now()
	var attempted []string
	for _, d := range f.Destinations {
		if p.pending(m.Attempts[d.Name], now) {
			if err := ob.Deliver(ctx, id, d.Name); err != nil {
				return Result{}, err
			}
			attempted = append(attempted, d.Name)
		}
	}
	latest, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	return p.complete(ctx, id, p.rollup(latest, f), nil, retry, attempted)
}

// pending reports whether a destination still needs a delivery attempt now:
// it has not succeeded, has attempts left, and any backoff has elapsed.
func (p *Pipeline) pending(a state.DestinationAttempt, now time.Time) bool {
	switch {
	case a.Attempts > 0 && a.LastError == "":
		return false // delivered
	case a.Attempts >= p.opts.MaxAttempts:
		return false // exhausted
	case a.NextAttemptAt.After(now):
		return false // waiting out backoff
	}
	return true
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

// retryPage is how many messages RetryDue loads per store query.
const retryPage = 200

// RetryDue resumes every message that is not in flight and has work left:
// queued messages whose destinations are due, and messages a crash left in
// "received" or "transformed". Deliveries reuse the message's idempotency
// keys. Each message is finished without cancellation once started; ctx
// cancellation stops the pass between messages. An error on one message is
// collected and the pass continues. It returns how many messages it resumed.
func (p *Pipeline) RetryDue(ctx context.Context, lookup FlowLookup) (int, error) {
	var errs []error
	n := 0
	for _, status := range []state.Status{state.StatusQueued, state.StatusTransformed, state.StatusReceived} {
		cursor := ""
		for {
			if ctx.Err() != nil {
				return n, errors.Join(errs...)
			}
			page, err := p.store.Search(ctx, state.Query{Status: status, IDFrom: cursor, Sort: "id", Limit: retryPage})
			if err != nil {
				return n, errors.Join(append(errs, err)...)
			}
			for _, m := range page {
				if ctx.Err() != nil {
					return n, errors.Join(errs...)
				}
				resumed, err := p.retryOne(context.WithoutCancel(ctx), m, lookup)
				if resumed {
					n++
				}
				if err != nil {
					errs = append(errs, fmt.Errorf("message %s: %w", m.ID, err))
				}
			}
			if len(page) < retryPage {
				break
			}
			cursor = page[len(page)-1].ID + "\x00" // IDFrom is inclusive
		}
	}
	return n, errors.Join(errs...)
}

// retryOne resumes one message if it is not in flight, its flow is running,
// and it has due work. Messages whose flow was deleted are dead-lettered.
func (p *Pipeline) retryOne(ctx context.Context, m state.Message, lookup FlowLookup) (bool, error) {
	if _, busy := p.inflight.LoadOrStore(m.ID, struct{}{}); busy {
		return false, nil
	}
	defer p.inflight.Delete(m.ID)
	if p.opts.Gate != nil {
		defer p.opts.Gate.ProcessFlow(m.FlowID)()
	}
	f, err := lookup(ctx, m.FlowID)
	if errors.Is(err, ErrFlowGone) {
		// No observer call: the flow's counters were cleared on delete.
		latest, err := p.store.Get(ctx, m.ID)
		if err != nil {
			return true, err
		}
		latest.Status = state.StatusDeadLettered
		if latest.Metadata == nil {
			latest.Metadata = map[string]string{}
		}
		latest.Metadata["error"] = ErrFlowGone.Error()
		return true, p.store.Put(ctx, latest)
	}
	if err != nil || f.Paused {
		return false, err
	}
	if m.Status == state.StatusQueued {
		now, due := time.Now(), false
		for _, d := range f.Destinations {
			due = due || p.pending(m.Attempts[d.Name], now)
		}
		if !due {
			// Nothing to deliver; fix the status if a crash hit between the
			// last delivery and the rollup (e.g. a destination exhausted).
			if status := p.rollup(m, f); status != state.StatusQueued {
				_, err := p.complete(ctx, m.ID, status, nil, true, nil)
				return true, err
			}
			return false, nil
		}
	}
	_, err = p.resume(ctx, f, m.ID, true)
	return true, err
}

// complete stores the message's final status for this pass (and a
// processing error in its "error" metadata) and tells the observer.
func (p *Pipeline) complete(ctx context.Context, id string, status state.Status, cause error, retry bool, attempted []string) (Result, error) {
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
		if retry {
			p.observer.Retried(m, attempted)
		} else {
			p.observer.Processed(m)
		}
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
