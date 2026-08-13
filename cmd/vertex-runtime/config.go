package main

import (
	"fmt"
	"os"
)

// Environment variable names read by the runtime.
const (
	envPackJSON  = "PROMPTPACK_PACK_JSON"
	envPackURI   = "PROMPTPACK_PACK_URI"
	envAgentName = "PROMPTPACK_AGENT"
	envProviders = "PROMPTPACK_PROVIDERS"
	envProject   = "PROMPTPACK_PROJECT"
	envLocation  = "PROMPTPACK_LOCATION"
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
	PackJSON      string
	PackURI       string
	PackFile      string
	AgentName     string
	ProvidersJSON string
	Project       string
	Location      string
	Port          int
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

	return cfg, nil
}
