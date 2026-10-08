package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/weavster-dev/weavster/internal/observability"
)

// serverMetrics reads the server's own counters when Prometheus scrapes
// (#107 D-91), so the metrics always agree with the flow statistics.
type serverMetrics struct {
	stats  *observability.StatsRegistry
	flows  flowLister
	limit  *processLimit // nil without a message store
	logger *slog.Logger
}

var (
	flowMessagesDesc = prometheus.NewDesc("weavster_flow_messages_total",
		"Messages of a flow by outcome since the server started (or the lifetime statistics were reset).",
		[]string{"flow", "outcome"}, nil)
	connectorMessagesDesc = prometheus.NewDesc("weavster_connector_messages_total",
		"Deliveries to a flow's destination by outcome since the server started (or the lifetime statistics were reset).",
		[]string{"flow", "connector", "outcome"}, nil)
	flowsDesc = prometheus.NewDesc("weavster_flows",
		"Flows by lifecycle status.", []string{"status"}, nil)
	inFlightDesc = prometheus.NewDesc("weavster_processing_in_flight",
		"Messages from the API and flow sources being received and processed now (retries and flow-to-flow hand-offs take no slot).", nil, nil)
	slotsDesc = prometheus.NewDesc("weavster_processing_slots",
		"Messages that can be processed at once (processing.maxConcurrent).", nil, nil)
	refusedDesc = prometheus.NewDesc("weavster_processing_refused_total",
		"Messages refused as busy because every processing slot was taken.", nil, nil)
)

func (m serverMetrics) Describe(ch chan<- *prometheus.Desc) {
	for _, d := range []*prometheus.Desc{flowMessagesDesc, connectorMessagesDesc, flowsDesc, inFlightDesc, slotsDesc, refusedDesc} {
		ch <- d
	}
}

// Collect exports every existing flow, with zeros before its first message
// and after a reset, as the flow statistics show them; a deleted flow is
// gone.
func (m serverMetrics) Collect(ch chan<- prometheus.Metric) {
	counter := func(d *prometheus.Desc, v int64, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, float64(v), labels...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if flows, err := m.flows.List(ctx); err != nil {
		m.logger.Warn("metrics: flows could not be listed; the flow metrics are left out of this scrape", "error", err)
	} else {
		all := m.stats.SnapshotAll(true) // one instant for every flow
		byStatus := map[string]int{}
		for _, f := range flows {
			byStatus[f.Status]++
			st := all[f.ID]
			for outcome, v := range map[string]int64{"received": st.Received, "filtered": st.Filtered, "transformed": st.Transformed,
				"sent": st.Sent, "errored": st.Errored, "queued": st.Queued} {
				counter(flowMessagesDesc, v, f.ID, outcome)
			}
			destinations := map[string]bool{}
			for _, d := range f.Destinations {
				destinations[d.Name] = true
			}
			for name := range st.Connectors {
				destinations[name] = true // removed from the flow since
			}
			for name := range destinations {
				c := st.Connectors[name]
				counter(connectorMessagesDesc, c.Sent, f.ID, name, "sent")
				counter(connectorMessagesDesc, c.Errored, f.ID, name, "errored")
			}
		}
		for status, n := range byStatus {
			ch <- prometheus.MustNewConstMetric(flowsDesc, prometheus.GaugeValue, float64(n), status)
		}
	}
	if m.limit != nil {
		inFlight, slots, refused := m.limit.usage()
		ch <- prometheus.MustNewConstMetric(inFlightDesc, prometheus.GaugeValue, float64(inFlight))
		ch <- prometheus.MustNewConstMetric(slotsDesc, prometheus.GaugeValue, float64(slots))
		counter(refusedDesc, refused)
	}
}

// metricsHandler serves the server's metrics and the Go runtime and
// process metrics.
func metricsHandler(m serverMetrics) http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(m, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}
