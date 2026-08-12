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
	envProject   = "GOOGLE_CLOUD_PROJECT"
	envLocation  = "GOOGLE_CLOUD_LOCATION"
)

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
		Project:       os.Getenv(envProject),
		Location:      os.Getenv(envLocation),
		Port:          contractPort,
	}

	if cfg.PackJSON == "" && cfg.PackURI == "" {
		return nil, fmt.Errorf("%s or %s is required", envPackJSON, envPackURI)
	}

	return cfg, nil
}
