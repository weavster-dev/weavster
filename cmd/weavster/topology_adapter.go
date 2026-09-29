package main

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/weavster-dev/weavster/internal/compiler"
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
	snap := t.snapshot()
	summaries := make([]topology.FlowSummary, 0, len(flows))
	for _, f := range flows {
		st := t.state(f, snap)
		var routes []topology.Link
		for _, d := range f.Destinations {
			if d.Type != "flow" || d.Flow == "" {
				continue
			}
			i := slices.IndexFunc(routes, func(l topology.Link) bool { return l.Flow == d.Flow })
			if i < 0 {
				routes = append(routes, topology.Link{Flow: d.Flow})
				i = len(routes) - 1
			}
			if slices.Contains(f.StoppedDestinations, d.Name) {
				continue // nothing crosses a stopped destination
			}
			cur, rec := st.connector(d.Name)
			routes[i].Activity = add(routes[i].Activity, st.connectorActivity(cur))
			routes[i].Status = mergeEdge(routes[i].Status, st.edge(rec.Sent, rec.Errored))
		}
		for i := range routes {
			if routes[i].Status == "" {
				routes[i].Status = "idle"
			}
		}
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
	st := t.state(f, t.snapshot())
	status := st.status(st.recent.Sent, st.recent.Errored)
	detail := topology.FlowDetail{ID: f.ID, Name: f.Name, Status: status}
	if src := f.Source; src != nil {
		format := f.InputFormat
		if format == "" {
			format = "json"
		}
		part := &topology.Part{ID: src.Type, Label: sourceLabel(f), Status: st.lifecycle, Meta: map[string]string{"connectorType": src.Type, "dataType": format},
			EdgeStatus: st.edge(st.recent.Received, 0)}
		if st.current != nil { // every message the flow received: from the source and through the API
			part.Activity = &topology.Activity{Received: st.current.Received, LastMessageAt: st.lastMessage()}
		}
		detail.Source = part
	}
	if name, steps, ok := transformSummary(f.Transform); ok {
		detail.Transform = &topology.Part{ID: "dsl:" + name, Label: name, Status: status, Activity: st.activity(),
			Meta: map[string]string{"steps": strconv.Itoa(steps)}}
	}
	for _, d := range f.Destinations {
		cur, rec := st.connector(d.Name)
		part := topology.Part{ID: d.Name, Label: d.Name, Meta: map[string]string{"connectorType": d.Type},
			Status: st.status(rec.Sent, rec.Errored), EdgeStatus: st.edge(rec.Sent, rec.Errored), Activity: st.connectorActivity(cur)}
		if slices.Contains(f.StoppedDestinations, d.Name) {
			part.EdgeStatus = "idle" // nothing crosses a stopped destination
			if st.started {
				part.Status = "stopped"
			}
		}
		if d.Type == "flow" {
			part.Meta["flow"] = d.Flow
			detail.Routes = append(detail.Routes, topology.Route{Destination: d.Name, Flow: d.Flow, Status: part.EdgeStatus, Activity: part.Activity})
		}
		detail.Destinations = append(detail.Destinations, part)
	}
	return topology.FlowInternal(detail), nil
}

// statsSnapshot is every flow's current and lifetime statistics, taken at
// one instant for a whole graph.
type statsSnapshot struct {
	current, lifetime map[string]observability.FlowStats
}

func (t topologyAdapter) snapshot() statsSnapshot {
	if t.stats == nil {
		return statsSnapshot{}
	}
	cur, life := t.stats.Snapshots()
	return statsSnapshot{current: cur, lifetime: life}
}

// flowState is what the topology shows of one flow's traffic: its current
// counters, and what was counted in the last topologyWindow (ok false when
// no sample covers it).
type flowState struct {
	lifecycle string // flowlife state
	started   bool
	current   *observability.FlowStats // nil: no statistics
	recent    observability.FlowStats
	ok        bool
}

func (t topologyAdapter) state(f gateway.Flow, snap statsSnapshot) flowState {
	lifecycle := flowlife.Normalize(f.Status)
	st := flowState{lifecycle: lifecycle, started: lifecycle == flowlife.Started}
	if snap.current == nil {
		return st
	}
	cur := snap.current[f.ID]
	st.current = &cur
	if t.series == nil {
		return st
	}
	now := time.Now()
	if t.now != nil {
		now = t.now()
	}
	cutoff := now.Add(-topologyWindow)
	keep := func(id string) bool { return id == f.ID }
	// The newest sample at or before the window's start, then those in it.
	points := t.series.Series(keep, time.Time{}, cutoff.Add(-time.Nanosecond), 1)
	points = append(points, t.series.Series(keep, cutoff, time.Time{}, 0)...)
	if len(points) == 0 {
		return st
	}
	counts := make([]observability.FlowStats, 0, len(points)+1)
	for _, p := range points {
		counts = append(counts, p.Stats)
	}
	st.recent, st.ok = increase(append(counts, snap.lifetime[f.ID])), true
	return st
}

// increase adds up what was counted from one lifetime snapshot to the
// next, per flow and per connector; a counter that went down (a lifetime
// reset) counts from zero.
func increase(counts []observability.FlowStats) observability.FlowStats {
	d := func(a, b int64) int64 {
		if b < a {
			return b
		}
		return b - a
	}
	out := observability.FlowStats{Connectors: map[string]observability.ConnectorStats{}}
	for i := 1; i < len(counts); i++ {
		a, b := counts[i-1], counts[i]
		out.Received += d(a.Received, b.Received)
		out.Sent += d(a.Sent, b.Sent)
		out.Errored += d(a.Errored, b.Errored)
		out.Queued += d(a.Queued, b.Queued)
		for name, cb := range b.Connectors {
			ca, c := a.Connectors[name], out.Connectors[name]
			out.Connectors[name] = observability.ConnectorStats{Received: c.Received + d(ca.Received, cb.Received), Sent: c.Sent + d(ca.Sent, cb.Sent),
				Errored: c.Errored + d(ca.Errored, cb.Errored), Queued: c.Queued + d(ca.Queued, cb.Queued)}
		}
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

// mergeEdge combines the statuses of two edges drawn as one.
func mergeEdge(a, b string) string {
	for _, s := range []string{"errored", "active", "idle"} {
		if a == s || b == s {
			return s
		}
	}
	return ""
}

func (st flowState) activity() *topology.Activity {
	if st.current == nil {
		return nil
	}
	c := st.current
	return &topology.Activity{Received: c.Received, Sent: c.Sent, Errored: c.Errored, Queued: c.Queued, LastMessageAt: st.lastMessage()}
}

func (st flowState) connectorActivity(c observability.ConnectorStats) *topology.Activity {
	if st.current == nil {
		return nil
	}
	return &topology.Activity{Received: c.Received, Sent: c.Sent, Errored: c.Errored, Queued: c.Queued}
}

// add sums two activities (nil when both are).
func add(a, b *topology.Activity) *topology.Activity {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &topology.Activity{Received: a.Received + b.Received, Sent: a.Sent + b.Sent, Errored: a.Errored + b.Errored, Queued: a.Queued + b.Queued}
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
	var t compiler.Transform
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &t) != nil {
		return "", 0, false
	}
	if t.Name == "" {
		t.Name = "transform"
	}
	return t.Name, len(t.Steps), true
}
