package observability

import (
	"context"
	"testing"
)

// TestTracerProviderDefaultExporter covers the len(exporters)==0 fallback
// branch: when neither Stdout nor OTLPEndpoint is configured, NewTracerProvider
// must synthesize a default stdout exporter rather than returning a provider
// with no span exporter.
func TestTracerProviderDefaultExporter(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerOptions{})
	if err != nil {
		t.Fatalf("NewTracerProvider with no options: %v", err)
	}
	if tp == nil {
		t.Fatal("NewTracerProvider returned nil provider")
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// TestTracerProviderOTLPExporter covers the OTLPEndpoint != "" branch, which
// constructs an OTLP-HTTP span exporter. Construction must not require a live
// collector (no spans are exported), so this is unit-testable offline.
func TestTracerProviderOTLPExporter(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerOptions{OTLPEndpoint: "localhost:4318"})
	if err != nil {
		t.Fatalf("NewTracerProvider with OTLP endpoint: %v", err)
	}
	if tp == nil {
		t.Fatal("NewTracerProvider returned nil provider")
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// TestTracerProviderBothExporters covers configuring stdout and OTLP together
// (multiple batched exporters on a single provider).
func TestTracerProviderBothExporters(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerOptions{
		Stdout:       true,
		OTLPEndpoint: "localhost:4318",
	})
	if err != nil {
		t.Fatalf("NewTracerProvider with both exporters: %v", err)
	}
	if tp == nil {
		t.Fatal("NewTracerProvider returned nil provider")
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
