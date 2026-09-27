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
	"mime"
	"net/url"
	"strconv"
	"strings"
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
	// Stopped destinations are not delivered to; their messages stay queued
	// without using attempts until the destination is started.
	Stopped bool
	Name    string
	Type    string // "http" or "file"
	URL     string // http
	Dir     string // file
	// Method, Timeout, and MaxRedirects shape http requests (zero: defaults).
	Method       string
	Timeout      time.Duration
	MaxRedirects int
	// Transform, when set, runs on the flow's output before delivery to
	// this destination; its filter steps drop the message for it alone.
	Transform *compiler.Transform
	// ResponseTransform, when set, runs on this destination's reply.
	ResponseTransform *compiler.Transform
}

// Flow is the processing definition the pipeline runs.
type Flow struct {
	ID           string
	Paused       bool                // stopped, paused, halted, or undeployed: RetryDue skips its messages
	Transform    *compiler.Transform // nil passes messages through unchanged
	Destinations []Destination
	// ResponseSelector names the destination whose reply Process returns.
	ResponseSelector string
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

// Reply is a destination's response to a successful delivery.
type Reply struct {
	Body        []byte
	ContentType string // MIME type of Body, as the destination declared it
}

// ResponseSink is a Sink whose destination replies (HTTP). The reply is nil
// when the destination sent none it could use; the delivery still succeeded.
type ResponseSink interface {
	WriteResponse(ctx context.Context, d Delivery) (*Reply, error)
}

// SinkFactory builds the sink for a destination.
type SinkFactory func(Destination) (Sink, error)

// Result is the outcome of processing one message.
type Result struct {
	ID     string
	Status state.Status
	// Response is the selected destination's (transformed) reply when it
	// was delivered during this call; nil otherwise.
	Response json.RawMessage
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

func (p *Pipeline) outbox(f Flow, contentType string, outs map[string]destinationResult, reply **Reply) *outbox.Outbox {
	return outbox.New(p.store, p.deliverFunc(f, contentType, outs, reply), outbox.Options{MaxAttempts: p.opts.MaxAttempts, BackoffBase: p.opts.BackoffBase})
}

// Validate checks a flow definition: the transform compiles and every
// destination is complete and uniquely named.
func Validate(f Flow) error {
	if f.Transform != nil {
		if _, err := dsl.Compile(*f.Transform); err != nil {
			return err
		}
	}
	seen := map[string]string{} // name -> type
	for i, d := range f.Destinations {
		switch {
		case d.Name == "":
			return fmt.Errorf("destinations[%d]: name is required", i)
		case seen[d.Name] != "":
			return fmt.Errorf("destinations[%d]: duplicate name %q", i, d.Name)
		case d.Type == "http" && !validHTTPURL(d.URL):
			return fmt.Errorf("destination %s: url must be an absolute http:// or https:// URL, got %q", d.Name, d.URL)
		case d.Type == "file" && d.Dir == "":
			return fmt.Errorf("destination %s: dir is required for type file", d.Name)
		case d.Type != "http" && d.Type != "file":
			return fmt.Errorf("destination %s: type must be http or file, got %q", d.Name, d.Type)
		}
		if d.Transform != nil {
			if _, err := dsl.Compile(*d.Transform); err != nil {
				return fmt.Errorf("destination %s: transform: %w", d.Name, err)
			}
		}
		if d.ResponseTransform != nil {
			if d.Name != f.ResponseSelector {
				return fmt.Errorf("destination %s: responseTransform is only used on the responseSelector destination", d.Name)
			}
			if _, err := dsl.Compile(*d.ResponseTransform); err != nil {
				return fmt.Errorf("destination %s: responseTransform: %w", d.Name, err)
			}
		}
		seen[d.Name] = d.Type
	}
	switch kind := seen[f.ResponseSelector]; {
	case f.ResponseSelector == "":
	case kind == "":
		return fmt.Errorf("responseSelector: no destination named %q", f.ResponseSelector)
	case kind != "http":
		return fmt.Errorf("responseSelector: destination %s is type %s, which sends no reply; select an http destination", f.ResponseSelector, kind)
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
func (p *Pipeline) Process(ctx context.Context, f Flow, body []byte) (Result, error) {
	return p.ProcessWithMetadata(ctx, f, body, nil)
}

// ErrInFlight is returned by Remove for a message that is being processed
// or retried.
var ErrInFlight = errors.New("pipeline: message is being processed")

// ProcessWithMetadata is Process that stores metadata with the message from
// the start (for example where a reprocessed message came from).
func (p *Pipeline) ProcessWithMetadata(ctx context.Context, f Flow, body []byte, metadata map[string]string) (_ Result, err error) {
	if f.Transform != nil {
		if _, err := dsl.Compile(*f.Transform); err != nil {
			return Result{}, err
		}
	}
	if needsObject(f) {
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
	m := state.Message{ID: id, FlowID: f.ID, ContentType: contentType, Raw: body, Original: body, Metadata: metadata}
	if err := p.outbox(f, contentType, nil, nil).Receive(ctx, m); err != nil {
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
	res, err := p.resume(ctx, f, id, false)
	if err != nil {
		res.ID = id // stored: the caller must not send the same content again
	}
	return res, err
}

// needsObject reports whether messages of f must be JSON objects: the flow
// or one of its destinations has a transform.
func needsObject(f Flow) bool {
	if f.Transform != nil {
		return true
	}
	for _, d := range f.Destinations {
		if d.Transform != nil {
			return true
		}
	}
	return false
}

// Hold reserves a message id as in flight, so no processing or retry starts
// on it until release is called; ok is false while it is already busy.
func (p *Pipeline) Hold(id string) (release func(), ok bool) {
	if _, busy := p.inflight.LoadOrStore(id, struct{}{}); busy {
		return nil, false
	}
	return func() { p.inflight.Delete(id) }, true
}

// Remove deletes a stored message unless it is being processed or retried
// (ErrInFlight); while it is removed, no retry can start on it.
func (p *Pipeline) Remove(ctx context.Context, id string) error {
	return p.remove(ctx, id, false)
}

// RemoveDeadLettered is Remove for a dead-lettered message only: any other
// status is ErrNotDeadLettered, checked while the message is held.
func (p *Pipeline) RemoveDeadLettered(ctx context.Context, id string) error {
	return p.remove(ctx, id, true)
}

func (p *Pipeline) remove(ctx context.Context, id string, deadLetteredOnly bool) error {
	if _, busy := p.inflight.LoadOrStore(id, struct{}{}); busy {
		return ErrInFlight
	}
	defer p.inflight.Delete(id)
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if deadLetteredOnly && m.Status != state.StatusDeadLettered {
		return fmt.Errorf("%w (status %s)", ErrNotDeadLettered, m.Status)
	}
	return p.store.Delete(ctx, id)
}

// ErrNotDeadLettered is returned by Requeue for a message that is not
// dead-lettered.
var ErrNotDeadLettered = errors.New("pipeline: message is not dead-lettered")

// Requeue gives a dead-lettered message another round of delivery
// attempts: every destination that has not delivered starts again with no
// attempts, delivered destinations keep their record (they are never sent
// again), the processing error is cleared, the "requeues" metadata counts
// the requeue, and the message is queued for the next retry pass — or, if
// it was never transformed (its flow was deleted before that), put back to
// received so the retry pass transforms it first. It returns the message as
// it was before (ErrInFlight while the message is busy, ErrNotDeadLettered
// with the current status for any other status).
func (p *Pipeline) Requeue(ctx context.Context, id string) (state.Message, error) {
	if _, busy := p.inflight.LoadOrStore(id, struct{}{}); busy {
		return state.Message{}, ErrInFlight
	}
	defer p.inflight.Delete(id)
	m, err := p.store.Get(ctx, id)
	if err != nil {
		return state.Message{}, err
	}
	if m.Status != state.StatusDeadLettered {
		return state.Message{}, fmt.Errorf("%w (status %s)", ErrNotDeadLettered, m.Status)
	}
	before := m
	before.Attempts = make(map[string]state.DestinationAttempt, len(m.Attempts))
	before.Metadata = make(map[string]string, len(m.Metadata))
	attempts := make(map[string]state.DestinationAttempt, len(m.Attempts))
	for dest, a := range m.Attempts {
		before.Attempts[dest] = a
		if a.Attempts > 0 && a.LastError == "" {
			attempts[dest] = a // delivered
		}
	}
	md := make(map[string]string, len(m.Metadata)+1)
	for k, v := range m.Metadata {
		before.Metadata[k] = v
		if k != "error" {
			md[k] = v
		}
	}
	n, _ := strconv.Atoi(md["requeues"])
	md["requeues"] = strconv.Itoa(n + 1)
	m.Attempts, m.Metadata, m.Status = attempts, md, state.StatusQueued
	if m.Transformed == nil {
		m.Status = state.StatusReceived
	}
	return before, p.store.Put(ctx, m)
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
	ob := p.outbox(f, m.ContentType, nil, nil)
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
		m.Transformed = transformed
	}

	now := time.Now()
	outs := destinationOutputs(f, m)
	skip := filteredSet(outs)
	// Only a delivery made while the sender waits (not a retry) replies.
	var reply *Reply
	replyTo := &reply
	if retry || f.ResponseSelector == "" {
		replyTo = nil
	}
	dob := p.outbox(f, m.ContentType, outs, replyTo)
	var attempted []string
	for _, d := range f.Destinations {
		if !d.Stopped && !skip[d.Name] && p.pending(m.Attempts[d.Name], now) {
			if err := dob.Deliver(ctx, id, d.Name); err != nil {
				return Result{}, err
			}
			attempted = append(attempted, d.Name)
		}
	}
	latest, err := p.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	res, err := p.complete(ctx, id, p.rollup(latest, f, skip), nil, retry, attempted)
	if err == nil && reply != nil {
		res.Response = responseOutput(f, reply)
	}
	return res, err
}

// responseOutput is the selected destination's reply as returned to the
// sender: the response transform's result; without one, a JSON reply
// (declared application/json or +json) as is and any other reply as a JSON
// string. An empty reply, or one the transform cannot use (not a JSON
// object, a failing step) or filters, returns nil.
// isJSONNull reports whether b is the JSON literal null.
func isJSONNull(b []byte) bool { return bytes.Equal(bytes.TrimSpace(b), []byte("null")) }

func responseOutput(f Flow, r *Reply) json.RawMessage {
	var t *compiler.Transform
	for _, d := range f.Destinations {
		if d.Name == f.ResponseSelector {
			t = d.ResponseTransform
		}
	}
	if t != nil {
		out, _, err := destinationOutput(*t, r.Body)
		if err != nil || isJSONNull(out) {
			return nil
		}
		return out
	}
	switch {
	case len(r.Body) == 0:
		return nil
	case isJSONType(r.ContentType) && json.Valid(r.Body):
		if isJSONNull(r.Body) { // a null reply is no response
			return nil
		}
		return r.Body
	}
	s, _ := json.Marshal(string(r.Body)) // a string always encodes
	return s
}

// isJSONType reports whether a Content-Type declares JSON.
func isJSONType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "application/json" || strings.HasSuffix(mt, "+json"))
}

// destinationResult is one destination's transform of a message's flow
// output: the body to deliver, or that its filter dropped the message, or
// the transform's error (a delivery failure).
type destinationResult struct {
	body     []byte
	filtered bool
	err      error
}

// destinationOutputs runs, once, the transform of every destination of f
// that has one and still has work for m (not yet delivered). Stopped
// destinations are left out: they hold the message, even one their filter
// would drop, until started (their definition may change meanwhile). The
// transforms are deterministic, so every retry gets the same result.
func destinationOutputs(f Flow, m state.Message) map[string]destinationResult {
	outs := map[string]destinationResult{}
	for _, d := range f.Destinations {
		a := m.Attempts[d.Name]
		if d.Transform == nil || d.Stopped || (a.Attempts > 0 && a.LastError == "") {
			continue
		}
		var r destinationResult
		r.body, r.filtered, r.err = destinationOutput(*d.Transform, m.Transformed)
		outs[d.Name] = r
	}
	return outs
}

// destinationOutput runs transform t over body, the flow's output.
func destinationOutput(t compiler.Transform, body []byte) (out []byte, filtered bool, err error) {
	prog, err := dsl.Compile(t)
	if err != nil {
		return nil, false, err
	}
	doc, err := decodeObject(body)
	if err != nil {
		return nil, false, err
	}
	res, filtered, err := prog.Run(doc)
	if err != nil || filtered {
		return nil, filtered, err
	}
	out, err = json.Marshal(res)
	return out, false, err
}

// filteredSet lists the destinations whose own filter dropped the message.
func filteredSet(outs map[string]destinationResult) map[string]bool {
	skip := map[string]bool{}
	for name, r := range outs {
		if r.filtered {
			skip[name] = true
		}
	}
	return skip
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
// sink once per message. A destination with a transform receives its entry
// of outs.
// When reply is set, the reply to a successful delivery to the flow's
// response selector is stored there.
func (p *Pipeline) deliverFunc(f Flow, contentType string, outs map[string]destinationResult, reply **Reply) outbox.DeliverFunc {
	mime := "application/octet-stream"
	if contentType == "json" {
		mime = "application/json"
	}
	type built struct {
		transformed bool
		sink        Sink
		err         error
	}
	sinks := map[string]built{}
	for _, d := range f.Destinations {
		s, err := p.sinks(d)
		sinks[d.Name] = built{d.Transform != nil, s, err}
	}
	return func(ctx context.Context, m state.Message, dest, key string) error {
		b := sinks[dest]
		if b.err != nil {
			return b.err
		}
		body, destMime := m.Transformed, mime
		if b.transformed {
			out, ok := outs[dest]
			switch {
			case !ok || out.filtered: // callers skip filtered destinations
				return errors.New("destination transform: no output for this destination")
			case out.err != nil:
				return fmt.Errorf("destination transform: %w", out.err)
			}
			body, destMime = out.body, "application/json"
		}
		d := Delivery{MessageID: m.ID, Body: body, ContentType: destMime, IdempotencyKey: key}
		if rs, ok := b.sink.(ResponseSink); ok && reply != nil && dest == f.ResponseSelector {
			r, err := rs.WriteResponse(ctx, d)
			if err == nil {
				*reply = r
			}
			return err
		}
		return b.sink.Write(ctx, d)
	}
}

// rollup is queued while any destination still has work (a retry, an
// untried destination, or a stopped destination holding the message);
// otherwise dead-lettered if a destination exhausted its attempts, and sent
// when every destination succeeded or filtered the message. A message every
// destination filtered (none received it) is filtered.
func (p *Pipeline) rollup(m state.Message, f Flow, skip map[string]bool) state.Status {
	pending, exhausted := false, false
	filtered := 0
	for _, d := range f.Destinations {
		a := m.Attempts[d.Name]
		switch {
		case a.Attempts > 0 && a.LastError == "":
		case skip[d.Name]:
			filtered++
		case a.Attempts >= p.opts.MaxAttempts:
			exhausted = true
		default:
			pending = true
		}
	}
	switch {
	case pending:
		return state.StatusQueued
	case exhausted:
		return state.StatusDeadLettered
	case len(f.Destinations) > 0 && filtered == len(f.Destinations):
		return state.StatusFiltered
	}
	return state.StatusSent
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
		skip := filteredSet(destinationOutputs(f, m))
		for _, d := range f.Destinations {
			due = due || (!d.Stopped && !skip[d.Name] && p.pending(m.Attempts[d.Name], now))
		}
		if !due {
			// Nothing to deliver; fix the status if a crash hit between the
			// last delivery and the rollup (e.g. a destination exhausted).
			if status := p.rollup(m, f, skip); status != state.StatusQueued {
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
