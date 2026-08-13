package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestSetupTracing_DisabledIsANoop(t *testing.T) {
	shutdown, opts := setupTracing(&runtimeConfig{}, quietLogger())

	if len(opts) != 0 {
		t.Errorf("len(opts) = %d, want 0 when tracing is off", len(opts))
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown of a disabled tracer returned %v", err)
	}
}

// Enabling tracing without an endpoint is a misconfiguration, not a reason to
// fail the container: the agent should still serve.
func TestSetupTracing_EnabledWithoutEndpointIsANoop(t *testing.T) {
	shutdown, opts := setupTracing(
		&runtimeConfig{TracingEnabled: true}, quietLogger())

	if len(opts) != 0 {
		t.Errorf("len(opts) = %d, want 0 without an endpoint", len(opts))
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned %v", err)
	}
}

func TestSetupTracing_EnabledProducesAnOption(t *testing.T) {
	shutdown, opts := setupTracing(&runtimeConfig{
		TracingEnabled: true,
		OTLPEndpoint:   "localhost:4317",
	}, quietLogger())
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	if len(opts) != 1 {
		t.Fatalf("len(opts) = %d, want 1 SDK option wiring the tracer provider", len(opts))
	}
}
