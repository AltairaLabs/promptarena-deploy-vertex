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

	// ToolSpecs carries each tool's execution config as raw JSON. The compiled
	// pack has only tool schemas, so without forwarding these a deployed engine
	// receives tool calls it cannot fulfill. The runtime owns this schema; the
	// adapter makes no decisions on its contents, so modeling it here would
	// create a second definition to drift.
	ToolSpecs map[string]json.RawMessage `json:"tool_specs,omitempty"`
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

// encodeToolSpecs re-serializes the arena's tool specs for injection. Returns
// an empty string when the arena declares none, so the env var stays unset.
func encodeToolSpecs(a *ArenaConfig) (string, error) {
	if a == nil || len(a.ToolSpecs) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(a.ToolSpecs)
	if err != nil {
		return "", fmt.Errorf("encode tool specs: %w", err)
	}
	return string(encoded), nil
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
