package main

import (
	"fmt"
	"os"
	"strconv"
)

// Environment variable names read by the runtime.
const (
	envPackJSON  = "PROMPTPACK_PACK_JSON"
	envPackURI   = "PROMPTPACK_PACK_URI"
	envAgentName = "PROMPTPACK_AGENT"
	envProviders = "PROMPTPACK_PROVIDERS"
	envProject   = "PROMPTPACK_PROJECT"
	envLocation  = "PROMPTPACK_LOCATION"

	// envOTLPEndpoint is the standard OpenTelemetry variable, used as-is so the
	// image works with any OTLP collector rather than a name we invented.
	envOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"
	// envTracingEnabled gates tracing. Off unless explicitly set, so an
	// unconfigured deployment sends nothing and pays nothing.
	envTracingEnabled = "PROMPTPACK_TRACING_ENABLED"
)

// Fallback names for the GCP coordinates. Agent Runtime reserves these and
// rejects a deployment that sets them, but injects them into the container
// itself — so on Agent Runtime these are what actually resolve. The adapter
// sets the PROMPTPACK_-prefixed names above so the same image also runs on
// hosts that do not inject anything.
const (
	envProjectFallback  = "GOOGLE_CLOUD_PROJECT"
	envLocationFallback = "GOOGLE_CLOUD_LOCATION"
)

// firstEnv returns the first non-empty environment variable among names.
func firstEnv(names ...string) string {
	for _, name := range names {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// contractPort is fixed by the Agent Runtime contract: the container must
// listen for HTTP requests on 0.0.0.0 port 8080. It is not configurable.
const contractPort = 8080

// runtimeConfig holds all configuration parsed from environment variables.
type runtimeConfig struct {
	PackJSON       string
	PackURI        string
	PackFile       string
	AgentName      string
	ProvidersJSON  string
	Project        string
	Location       string
	Port           int
	OTLPEndpoint   string
	TracingEnabled bool
}

// loadConfig reads configuration from environment variables. Exactly one pack
// source is required: PROMPTPACK_PACK_JSON (inline) or PROMPTPACK_PACK_URI (gs://).
func loadConfig() (*runtimeConfig, error) {
	cfg := &runtimeConfig{
		PackJSON:      os.Getenv(envPackJSON),
		PackURI:       os.Getenv(envPackURI),
		AgentName:     os.Getenv(envAgentName),
		ProvidersJSON: os.Getenv(envProviders),
		Project:       firstEnv(envProject, envProjectFallback),
		Location:      firstEnv(envLocation, envLocationFallback),
		Port:          contractPort,
	}

	if cfg.PackJSON == "" && cfg.PackURI == "" {
		return nil, fmt.Errorf("%s or %s is required", envPackJSON, envPackURI)
	}

	cfg.OTLPEndpoint = os.Getenv(envOTLPEndpoint)

	if raw := os.Getenv(envTracingEnabled); raw != "" {
		enabled, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid %s %q: %w", envTracingEnabled, raw, parseErr)
		}
		cfg.TracingEnabled = enabled
	}

	return cfg, nil
}
