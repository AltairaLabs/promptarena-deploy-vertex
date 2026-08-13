package vertex

import (
	"encoding/json"
	"fmt"
)

// StateVersion is the current adapter state schema version. Bump it only for
// changes an older adapter cannot read.
const StateVersion = 1

// EngineState records one deployed Agent Runtime engine. The resource name
// cannot be derived from the pack because reasoningEngines IDs are
// server-assigned, so state is load-bearing for idempotency.
type EngineState struct {
	// ResourceName is the full projects/…/reasoningEngines/… name.
	ResourceName string `json:"resource_name"`
	// InFlight marks an engine whose creation was still running when the
	// adapter stopped waiting. A later apply reconciles it rather than
	// orphaning the resource.
	InFlight bool `json:"in_flight,omitempty"`
}

// State is the opaque adapter state the CLI persists between operations.
//
// Unknown fields survive a parse/marshal cycle so a newer adapter's state is
// not silently truncated by an older one reading and rewriting it.
type State struct {
	Version              int                    `json:"version"`
	AdapterVersion       string                 `json:"adapter_version,omitempty"`
	PackHash             string                 `json:"pack_hash,omitempty"`
	ConfigHash           string                 `json:"config_hash,omitempty"`
	ImageDigest          string                 `json:"image_digest,omitempty"`
	StagedPackURI        string                 `json:"staged_pack_uri,omitempty"`
	StagedPackGeneration int64                  `json:"staged_pack_generation,omitempty"`
	Engines              map[string]EngineState `json:"engines"`

	// unknown carries fields this adapter version does not recognize.
	unknown map[string]json.RawMessage
}

// newState returns an empty state at the current version.
func newState() *State {
	return &State{
		Version: StateVersion,
		Engines: map[string]EngineState{},
	}
}

// knownStateFields lists the JSON keys State owns. Anything else is preserved
// verbatim in unknown.
var knownStateFields = map[string]bool{
	"version":                true,
	"adapter_version":        true,
	"pack_hash":              true,
	"config_hash":            true,
	"image_digest":           true,
	"staged_pack_uri":        true,
	"staged_pack_generation": true,
	"engines":                true,
}

// parseState unmarshals prior adapter state. An empty string yields a fresh
// empty state, which is the first-deploy case.
func parseState(raw string) (*State, error) {
	if raw == "" {
		return newState(), nil
	}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("invalid adapter state JSON: %w", err)
	}

	var s State
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("invalid adapter state: %w", err)
	}

	if s.Version > StateVersion {
		return nil, fmt.Errorf(
			"adapter state version %d is newer than this adapter supports (%d); upgrade the adapter",
			s.Version, StateVersion)
	}

	if s.Engines == nil {
		s.Engines = map[string]EngineState{}
	}

	s.unknown = make(map[string]json.RawMessage)
	for k, v := range probe {
		if !knownStateFields[k] {
			s.unknown[k] = v
		}
	}

	return &s, nil
}

// Marshal serializes the state, re-emitting any unknown fields it preserved.
func (s *State) Marshal() (string, error) {
	type stateAlias State
	encoded, err := json.Marshal((*stateAlias)(s))
	if err != nil {
		return "", fmt.Errorf("marshal adapter state: %w", err)
	}

	if len(s.unknown) == 0 {
		return string(encoded), nil
	}

	var merged map[string]json.RawMessage
	if decodeErr := json.Unmarshal(encoded, &merged); decodeErr != nil {
		return "", fmt.Errorf("merge adapter state: %w", decodeErr)
	}
	for k, v := range s.unknown {
		merged[k] = v
	}

	out, err := json.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("marshal merged adapter state: %w", err)
	}
	return string(out), nil
}
