package observability

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func TestPrometheusCounters(t *testing.T) {
	p := NewPrometheus()
	c := p.Counter("weavster_test_received_total", "test")
	g := p.Gauge("weavster_test_queued", "test")
	c.Inc()
	c.Add(2)
	g.Set(5)
	g.Inc()
	if c == nil || g == nil {
		t.Fatal("nil metric")
	}
	// Handler must be servable without error.
	if p.Handler() == nil {
		t.Fatal("nil handler")
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestStatsRegistryResetAndDump(t *testing.T) {
	s := NewStatsRegistry()
	s.Inc("flow:a", Received)
	s.Inc("flow:a", Sent)
	s.IncConnector("flow:a", "tcp-1", Sent)

	snap := s.Snapshot("flow:a", false)
	if snap.Received != 1 || snap.Sent != 1 {
		t.Errorf("current snapshot = %+v", snap)
	}
	if snap.Connectors["tcp-1"].Sent != 1 {
		t.Errorf("connector stats = %+v", snap.Connectors)
	}

	// Reset current only; lifetime retains.
	s.Reset("flow:a", false)
	if got := s.Snapshot("flow:a", false); got.Received != 0 {
		t.Errorf("after reset current = %+v", got)
	}
	if got := s.Snapshot("flow:a", true); got.Received != 1 {
		t.Errorf("lifetime should retain, got %+v", got)
	}

	path := filepath.Join(t.TempDir(), "stats.json")
	if err := s.Dump(path, true); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dump file missing: %v", err)
	}
}

func TestEventLogSearchCount(t *testing.T) {
	l := NewEventLog()
	l.Add("flow.started", "admin", "flow:a", nil)
	l.Add("phi.access", "admin", "flow:a", map[string]string{"msg": "1"})
	l.Add("flow.started", "ops", "flow:b", nil)

	if got := l.Count(EventFilter{Type: "flow.started"}); got != 2 {
		t.Errorf("count flow.started = %d, want 2", got)
	}
	if got := l.Count(EventFilter{Flow: "flow:a"}); got != 2 {
		t.Errorf("count flow:a = %d, want 2", got)
	}
	before := time.Now().Add(time.Hour)
	if got := l.Count(EventFilter{Since: before}); got != 0 {
		t.Errorf("count since future = %d, want 0", got)
	}
}

func TestTimeSeries(t *testing.T) {
	ts := NewTimeSeries(2 * time.Minute)
	t0 := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ { // one snapshot a minute for flows a and b
		at := t0.Add(time.Duration(i) * time.Minute)
		ts.Record(at, "a", FlowStats{Received: int64(i)})
		ts.Record(at, "b", FlowStats{Received: int64(10 + i)})
	}
	for _, tt := range []struct {
		name     string
		flow     string
		from, to time.Time
		want     []int64
	}{
		{"retention drops older snapshots", "a", time.Time{}, time.Time{}, []int64{2, 3, 4}},
		{"every flow", "", time.Time{}, time.Time{}, []int64{2, 12, 3, 13, 4, 14}},
		{"from inclusive", "b", t0.Add(3 * time.Minute), time.Time{}, []int64{13, 14}},
		{"to inclusive", "a", time.Time{}, t0.Add(3 * time.Minute), []int64{2, 3}},
		{"unknown flow", "c", time.Time{}, time.Time{}, []int64{}},
	} {
		got := []int64{}
		for _, p := range ts.Series(tt.flow, tt.from, tt.to) {
			got = append(got, p.Stats.Received)
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestLogRing(t *testing.T) {
	r := NewLogRing(3)
	for _, line := range []string{"a", "b", "c", "d", "e"} {
		r.Add(line)
	}
	got := r.Tail(0)
	if len(got) != 3 {
		t.Errorf("tail length = %d, want 3", len(got))
	}
	if got[len(got)-1] != "e" {
		t.Errorf("last = %q, want e", got[len(got)-1])
	}
}

func TestTracerProvider(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerOptions{Stdout: true})
	if err != nil {
		t.Fatalf("tracer provider: %v", err)
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()
	otel.SetTracerProvider(tp)
	tr := tp.Tracer("weavster-test")
	_, span := tr.Start(context.Background(), "span")
	span.End()

	h := NoopSpanHook()
	ctx, done := h.Start(context.Background(), "m", "v", "f")
	done()
	if ctx == nil {
		t.Fatal("nil ctx")
	}
}

func TestLogger(t *testing.T) {
	l := NewLogger(os.Stderr, slog.LevelWarn)
	if l == nil {
		t.Fatal("nil logger")
	}
}
