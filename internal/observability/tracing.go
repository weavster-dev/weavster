package observability

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/trace"
)

// TracerOptions selects which span exporters to enable.
type TracerOptions struct {
	Stdout       bool
	OTLPEndpoint string
}

// NewTracerProvider builds an OTel SDK tracer provider with a stdout exporter
// and/or an OTLP-HTTP exporter (arch §3.1 MetricsExporter: OTel stdout/OTLP).
func NewTracerProvider(ctx context.Context, opts TracerOptions) (*trace.TracerProvider, error) {
	var exporters []trace.SpanExporter
	if opts.Stdout {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
	}
	if opts.OTLPEndpoint != "" {
		exp, err := otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(opts.OTLPEndpoint),
			otlptracehttp.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
	}
	if len(exporters) == 0 {
		exp, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		exporters = append(exporters, exp)
	}

	tpOpts := make([]trace.TracerProviderOption, 0, len(exporters))
	for _, e := range exporters {
		tpOpts = append(tpOpts, trace.WithBatcher(e))
	}
	return trace.NewTracerProvider(tpOpts...), nil
}

// SpanHook is the executor span hook present for future OTel propagation into
// the WASM guest (gap #3, deferred Enterprise). MVP provides a no-op hook.
type SpanHook interface {
	Start(ctx context.Context, module, version, flowID string) (context.Context, func())
}

type noopSpanHook struct{}

func (noopSpanHook) Start(ctx context.Context, _, _, _ string) (context.Context, func()) {
	return ctx, func() {}
}

// NoopSpanHook returns a span hook that records nothing.
func NoopSpanHook() SpanHook { return noopSpanHook{} }
