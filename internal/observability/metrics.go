// Package observability implements the MetricsExporter port (Prometheus + OTel),
// structured logging, events, and statistics.
package observability

import (
	"context"
	"net/http"
	"runtime"
	"time"

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

// SystemInfo is the system status payload (spec §2.11.38).
type SystemInfo struct {
	ID        string   `json:"id"`
	Version   string   `json:"version"`
	BuildDate string   `json:"buildDate"`
	Timezone  string   `json:"timezone"`
	Time      string   `json:"time"`
	Runtime   string   `json:"runtime"`
	Charsets  []string `json:"charsets"`
	Protocols []string `json:"protocols"`
	Ciphers   []string `json:"ciphers"`
	License   string   `json:"license"`
}

// SystemStatus returns the current system status (spec §2.11.38).
func SystemStatus(id, version, buildDate string) SystemInfo {
	now := time.Now()
	return SystemInfo{
		ID:        id,
		Version:   version,
		BuildDate: buildDate,
		Timezone:  time.Local.String(),
		Time:      now.Format(time.RFC3339),
		Runtime:   runtime.Version(),
		Charsets:  []string{"UTF-8", "ISO-8859-1"},
		Protocols: []string{"TLS 1.2", "TLS 1.3"},
		Ciphers: []string{
			"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
			"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
		},
		License: "MVP (no entitlement gating)",
	}
}

var _ MetricsExporter = (*Prometheus)(nil)
