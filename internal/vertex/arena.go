package vertex

import (
	"encoding/json"
	"fmt"
)

// ArenaConfig is the narrow view of the arena config this adapter needs. The
// CLI serializes the whole config into PlanRequest.ArenaConfig; only provider
// definitions matter here.
type ArenaConfig struct {
	LoadedProviders map[string]*ArenaProvider `json:"loaded_providers,omitempty"`
	ProviderSpecs   map[string]*ArenaProvider `json:"provider_specs,omitempty"`
}

// ArenaProvider is a provider definition from the arena config.
type ArenaProvider struct {
	ID    string `json:"id,omitempty"`
	Type  string `json:"type"`
	Model string `json:"model"`
}

// parseArenaConfig unmarshals the arena config JSON. An empty string yields a
// nil config, which callers treat as "no arena providers available".
func parseArenaConfig(raw string) (*ArenaConfig, error) {
	if raw == "" {
		return nil, nil
	}
	var cfg ArenaConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("invalid arena config JSON: %w", err)
	}
	return &cfg, nil
}

// provider looks up a provider by id, preferring loaded providers over specs.
// A nil receiver resolves nothing, so callers need not special-case it.
func (a *ArenaConfig) provider(id string) *ArenaProvider {
	if a == nil {
		return nil
	}
	if p, ok := a.LoadedProviders[id]; ok {
		return p
	}
	if p, ok := a.ProviderSpecs[id]; ok {
		return p
	}
	return nil
}
