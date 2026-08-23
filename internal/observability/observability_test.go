package observability

import (
	"context"
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
	ts := NewTimeSeries(3)
	for i := 0; i < 5; i++ {
		ts.Record("flow:a", FlowStats{Received: int64(i)})
	}
	got := ts.Series("flow:a")
	if len(got) != 3 {
		t.Errorf("series length = %d, want 3 (bounded)", len(got))
	}
	if got[0].Stats.Received != 2 {
		t.Errorf("oldest retained = %+v, want Received=2", got[0])
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

func TestSystemStatus(t *testing.T) {
	s := SystemStatus("weavster-1", "0.1.0", "2026-08-23")
	if s.ID != "weavster-1" || s.Version != "0.1.0" || s.BuildDate != "2026-08-23" {
		t.Errorf("system status = %+v", s)
	}
	if len(s.Protocols) == 0 || len(s.Ciphers) == 0 {
		t.Errorf("protocols/ciphers missing: %+v", s)
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
