// Package observability implements the MetricsExporter port (Prometheus + OTel),
// structured logging, events, and statistics.
package observability

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Counter is a monotonically increasing metric.
type Counter interface {
	Inc()
	Add(v float64)
}

// Gauge is a settable metric.
type Gauge interface {
	Set(v float64)
	Add(v float64)
	Inc()
	Dec()
}

// MetricsExporter is the port for exporting metrics (arch §3.1).
type MetricsExporter interface {
	Counter(name, help string) Counter
	Gauge(name, help string) Gauge
	Handler() http.Handler
	Shutdown(ctx context.Context) error
}

// Prometheus is the Prometheus MetricsExporter adapter.
type Prometheus struct {
	reg *prometheus.Registry
}

// NewPrometheus returns a Prometheus exporter with its own registry.
func NewPrometheus() *Prometheus {
	return &Prometheus{reg: prometheus.NewRegistry()}
}

func (p *Prometheus) Counter(name, help string) Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
	p.reg.MustRegister(c)
	return c
}

func (p *Prometheus) Gauge(name, help string) Gauge {
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help})
	p.reg.MustRegister(g)
	return g
}

func (p *Prometheus) Handler() http.Handler {
	return promhttp.HandlerFor(p.reg, promhttp.HandlerOpts{})
}

func (p *Prometheus) Shutdown(context.Context) error { return nil }

var _ MetricsExporter = (*Prometheus)(nil)
