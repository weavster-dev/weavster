package main

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/state"
)

// eventRepository keeps events (the store: PostgreSQL, SQLite, or memory).
type eventRepository interface {
	AppendEvents(ctx context.Context, events []state.EventRecord) error
	RecentEvents(ctx context.Context, n int) ([]state.EventRecord, int64, error)
	DeleteEventsBefore(ctx context.Context, t time.Time) (int, error)
}

// Event writer bounds (#107 D-94).
const (
	eventQueue      = 10000                  // events waiting to be stored
	eventBatch      = 200                    // events per write
	eventFlushEvery = 250 * time.Millisecond // how long an event can wait
	eventWriteLimit = 5 * time.Second        // one write
	eventDrainLimit = 5 * time.Second        // everything still queued at stop
)

// eventWriter stores the event log's events in the background, in
// batches, so recording an event never waits for the store. A full queue
// drops events from the store only (they stay in the log) and says so. It
// runs from start until stop, which the server calls just before it closes
// the store, after the API has drained, so the last events are stored too.
type eventWriter struct {
	repo    eventRepository
	logger  *slog.Logger
	queue   chan observability.Event
	dropped atomic.Int64
	every   time.Duration // eventFlushEvery
	log     *observability.EventLog
	cancel  context.CancelFunc
	done    chan struct{}
}

func newEventWriter(repo eventRepository, logger *slog.Logger) *eventWriter {
	return &eventWriter{repo: repo, logger: logger, queue: make(chan observability.Event, eventQueue), every: eventFlushEvery}
}

// restore loads the newest stored events into log and sends every new
// event to the writer. New ids start at least at the time in microseconds,
// so an id issued before a stop but not stored (a crash, a queue that was
// full) is never given to another event: ids only grow, and may jump.
func (w *eventWriter) restore(ctx context.Context, log *observability.EventLog, now time.Time) error {
	records, maxID, err := w.repo.RecentEvents(ctx, observability.MaxEvents)
	if err != nil {
		return err
	}
	events := make([]observability.Event, len(records))
	for i, r := range records {
		events[i] = event(r)
	}
	log.Load(events, max(maxID, now.UnixMicro()))
	log.SetSink(w.add)
	w.log = log
	return nil
}

// start runs the writer until stop.
func (w *eventWriter) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel, w.done = cancel, make(chan struct{})
	go func() { w.loop(ctx); close(w.done) }()
}

// stop stops sending events to the writer, writes what is queued (for at
// most eventDrainLimit), and waits until it has.
func (w *eventWriter) stop() {
	if w.log != nil {
		w.log.SetSink(nil)
	}
	if w.cancel != nil {
		w.cancel()
		<-w.done
	}
}

func (w *eventWriter) add(e observability.Event) {
	select {
	case w.queue <- e:
	default:
		w.dropped.Add(1)
	}
}

// loop writes queued events until ctx ends, then what is still queued.
func (w *eventWriter) loop(ctx context.Context) {
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	var batch []state.EventRecord
	for {
		select {
		case e := <-w.queue:
			batch = append(batch, record(e))
			if len(batch) >= eventBatch {
				batch = w.write(batch)
			}
		case <-ticker.C:
			batch = w.write(batch)
		case <-ctx.Done():
			w.drain(batch)
			return
		}
	}
}

// drain writes what is still queued, in batches, until the queue is empty
// or eventDrainLimit has passed; what is left then is reported as not
// stored.
func (w *eventWriter) drain(batch []state.EventRecord) {
	deadline := time.Now().Add(eventDrainLimit)
	for {
		select {
		case e := <-w.queue:
			batch = append(batch, record(e))
			if len(batch) < eventBatch {
				continue
			}
		default:
			w.write(batch)
			return
		}
		batch = w.write(batch)
		if time.Now().After(deadline) {
			w.dropped.Add(int64(len(w.queue)))
			for len(w.queue) > 0 {
				<-w.queue
			}
			w.write(nil) // reports the dropped ones
			return
		}
	}
}

func record(e observability.Event) state.EventRecord {
	return state.EventRecord{ID: e.ID, At: e.At, Type: e.Type, Actor: e.Actor, Flow: e.Flow, Data: e.Data}
}

func event(r state.EventRecord) observability.Event {
	return observability.Event{ID: r.ID, At: r.At, Type: r.Type, Actor: r.Actor, Flow: r.Flow, Data: r.Data}
}

// write stores batch (logging a failure) and reports dropped events; it
// returns the emptied batch for reuse.
func (w *eventWriter) write(batch []state.EventRecord) []state.EventRecord {
	if n := w.dropped.Swap(0); n > 0 {
		w.logger.Warn("events not stored: the store falls behind (they stay in the event log until the server stops)", "dropped", n)
	}
	if len(batch) == 0 {
		return batch
	}
	ctx, cancel := context.WithTimeout(context.Background(), eventWriteLimit)
	defer cancel()
	if err := w.repo.AppendEvents(ctx, batch); err != nil {
		w.logger.Warn("events not stored", "count", len(batch), "error", err)
	}
	return batch[:0]
}
