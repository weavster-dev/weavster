package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

// topologyWindow is how far back a started flow's recent deliveries are
// judged: errored when at least half of them failed (#107 D-99).
const topologyWindow = 5 * time.Minute

// topologyAdapter builds the topology graphs from the stored flow
// definitions, their statistics, and the statistics samples.
type topologyAdapter struct {
	flows  gateway.FlowStore
	stats  *observability.StatsRegistry // nil: no activity
	series *observability.TimeSeries    // nil: no errored status, edges idle
	now    func() time.Time             // nil: time.Now
}

func (t topologyAdapter) Overview(ctx context.Context) (topology.Graph, error) {
	flows, err := t.flows.List(ctx)
	if err != nil {
		return topology.Graph{}, err
	}
	summaries := make([]topology.FlowSummary, 0, len(flows))
	for _, f := range flows {
		var routes []string
		for _, d := range f.Destinations {
			if d.Type == "flow" && d.Flow != "" && !slices.Contains(routes, d.Flow) {
				routes = append(routes, d.Flow)
			}
		}
		st := t.state(f)
		summaries = append(summaries, topology.FlowSummary{ID: f.ID, Name: f.Name, Status: st.status(st.recent.Sent, st.recent.Errored),
			Activity: st.activity(), Routes: routes, Deps: f.DependsOn})
	}
	return topology.Overview(summaries), nil
}

// FlowInternal builds one flow's graph; id may be given as a or flow:a
// (#107 D-13).
func (t topologyAdapter) FlowInternal(ctx context.Context, id string) (topology.Graph, error) {
	f, err := t.flows.Get(ctx, strings.TrimPrefix(id, "flow:"))
	if err != nil {
		return topology.Graph{}, err
	}
	st := t.state(f)
	status := st.status(st.recent.Sent, st.recent.Errored)
	detail := topology.FlowDetail{ID: f.ID, Name: f.Name, Status: status}
	if typ := f.SourceKind(); typ != "" {
		format := f.InputFormat
		if format == "" {
			format = "json"
		}
		src := &topology.Part{ID: typ, Label: sourceLabel(f), Status: st.lifecycle, Meta: map[string]string{"connectorType": typ, "dataType": format},
			EdgeStatus: st.edge(st.recent.Received, 0)}
		if st.current != nil {
			src.Activity = &topology.Activity{Received: st.current.Received, LastMessageAt: st.lastMessage()}
		}
		detail.Source = src
	}
	if name, steps, ok := transformSummary(f.Transform); ok {
		detail.Transform = &topology.Part{ID: "dsl:" + name, Label: name, Status: status, Activity: st.activity(),
			Meta: map[string]string{"steps": strconv.Itoa(steps)}}
	}
	for _, d := range f.Destinations {
		cur, rec := st.connector(d.Name)
		part := topology.Part{ID: d.Name, Label: d.Name, Meta: map[string]string{"connectorType": d.Type}}
		if d.Type == "flow" {
			part.Meta["flow"] = d.Flow
			detail.Routes = append(detail.Routes, topology.Route{Destination: d.Name, Flow: d.Flow})
		}
		part.Status = st.status(rec.Sent, rec.Errored)
		if st.started && slices.Contains(f.StoppedDestinations, d.Name) {
			part.Status = "stopped"
		}
		part.EdgeStatus = st.edge(rec.Sent, rec.Errored)
		if st.current != nil {
			part.Activity = &topology.Activity{Received: cur.Received, Sent: cur.Sent, Errored: cur.Errored, Queued: cur.Queued}
		}
		detail.Destinations = append(detail.Destinations, part)
	}
	return topology.FlowInternal(detail), nil
}

// flowState is what the topology shows of one flow's traffic: its current
// counters, and the deliveries of the last topologyWindow (ok false when
// no sample is that recent).
type flowState struct {
	lifecycle string // flowlife state
	started   bool
	current   *observability.FlowStats // nil: no statistics
	recent    observability.FlowStats
	ok        bool
}

func (t topologyAdapter) state(f gateway.Flow) flowState {
	lifecycle := flowlife.Normalize(f.Status)
	st := flowState{lifecycle: lifecycle, started: lifecycle == flowlife.Started}
	if t.stats == nil {
		return st
	}
	cur := t.stats.Snapshot(f.ID, false)
	st.current = &cur
	if t.series == nil {
		return st
	}
	now := time.Now()
	if t.now != nil {
		now = t.now()
	}
	samples := t.series.Series(func(id string) bool { return id == f.ID }, now.Add(-topologyWindow), time.Time{}, 0)
	if len(samples) == 0 {
		return st
	}
	st.recent, st.ok = since(t.stats.Snapshot(f.ID, true), samples[0].Stats), true
	return st
}

// since is what was counted after base, per flow and per connector; a
// counter below base (a lifetime reset) counts from zero.
func since(now, base observability.FlowStats) observability.FlowStats {
	d := func(a, b int64) int64 {
		if a < b {
			return a
		}
		return a - b
	}
	out := observability.FlowStats{Received: d(now.Received, base.Received), Sent: d(now.Sent, base.Sent),
		Errored: d(now.Errored, base.Errored), Queued: d(now.Queued, base.Queued), Connectors: map[string]observability.ConnectorStats{}}
	for name, c := range now.Connectors {
		b := base.Connectors[name]
		out.Connectors[name] = observability.ConnectorStats{Received: d(c.Received, b.Received), Sent: d(c.Sent, b.Sent),
			Errored: d(c.Errored, b.Errored), Queued: d(c.Queued, b.Queued)}
	}
	return out
}

// status is the lifecycle status, or errored for a started flow when at
// least half of its recent deliveries failed.
func (st flowState) status(sent, errored int64) string {
	if st.started && st.ok && errored > 0 && 2*errored >= sent+errored {
		return "errored"
	}
	return st.lifecycle
}

// edge is an edge's status from the recent traffic across it.
func (st flowState) edge(passed, errored int64) string {
	switch {
	case !st.started || !st.ok || passed+errored == 0:
		return "idle"
	case 2*errored >= passed+errored:
		return "errored"
	}
	return "active"
}

func (st flowState) activity() *topology.Activity {
	if st.current == nil {
		return nil
	}
	c := st.current
	return &topology.Activity{Received: c.Received, Sent: c.Sent, Errored: c.Errored, Queued: c.Queued, LastMessageAt: st.lastMessage()}
}

func (st flowState) lastMessage() string {
	if st.current == nil || st.current.LastMessageAt == nil {
		return ""
	}
	return st.current.LastMessageAt.UTC().Format(time.RFC3339)
}

// connector is a destination's current and recent counters.
func (st flowState) connector(name string) (current, recent observability.ConnectorStats) {
	if st.current != nil {
		current = st.current.Connectors[name]
	}
	return current, st.recent.Connectors[name]
}

// sourceLabel names a flow's source by where messages come from, never
// with a secret.
func sourceLabel(f gateway.Flow) string {
	s := f.Source
	if s == nil {
		return f.SourceKind()
	}
	switch s.Type {
	case "file":
		if s.Pattern != "" {
			return "file:" + s.Dir + " (" + s.Pattern + ")"
		}
		return "file:" + s.Dir
	case "http":
		scheme := "http"
		if s.CertFile != "" {
			scheme = "https"
		}
		return scheme + "://" + s.Address + s.Path
	case "mllp":
		return "mllp://" + s.Address
	case "database":
		return "database:" + s.Driver
	}
	return s.Type
}

// transformSummary is a flow's transform: its name (transform when it has
// none) and number of steps; ok false when the flow has none.
func transformSummary(raw json.RawMessage) (name string, steps int, ok bool) {
	var t struct {
		Name  string            `json:"name"`
		Steps []json.RawMessage `json:"steps"`
	}
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &t) != nil {
		return "", 0, false
	}
	if t.Name == "" {
		t.Name = "transform"
	}
	return t.Name, len(t.Steps), true
}
