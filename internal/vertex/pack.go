package vertex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// a2aToolPrefix marks a tool that calls another agent. PromptKit namespaces
// these as a2a__<agent>__<skill> (runtime/tools/types.go).
const a2aToolPrefix = "a2a__"

// PackDelivery records how the pack reaches the runtime.
type PackDelivery struct {
	// Inline is true when the pack is injected as an environment variable
	// rather than staged to Cloud Storage.
	Inline bool
	// SizeBytes is the serialized pack size that drove the decision.
	SizeBytes int
}

// decidePackDelivery chooses between inline and staged delivery. A pack exactly
// at the limit is still inline; only exceeding it forces staging.
func decidePackDelivery(packJSON string, cfg *Config) PackDelivery {
	size := len(packJSON)
	return PackDelivery{Inline: size <= inlineLimit(cfg), SizeBytes: size}
}

// AgentInfo names one agent the deployment will serve.
type AgentInfo struct {
	Name    string
	IsEntry bool
}

// packView is the narrow slice of the pack this adapter reads. The full pack
// type lives in PromptKit; decoding only what is needed keeps the adapter
// tolerant of pack schema growth.
type packView struct {
	ID      string                     `json:"id"`
	Prompts map[string]json.RawMessage `json:"prompts"`
	Tools   map[string]json.RawMessage `json:"tools"`
	Agents  *packAgentsView            `json:"agents"`
}

// packAgentsView is the pack's agents section.
type packAgentsView struct {
	Entry   string                     `json:"entry"`
	Members map[string]json.RawMessage `json:"members"`
}

// decodePack unmarshals the narrow pack view.
func decodePack(packJSON string) (*packView, error) {
	var pack packView
	if err := json.Unmarshal([]byte(packJSON), &pack); err != nil {
		return nil, fmt.Errorf("invalid pack JSON: %w", err)
	}
	return &pack, nil
}

// enumerateAgents lists the agents to deploy, sorted by name so plans are
// stable across runs. A multi-agent pack yields one entry per member; a
// single-agent pack yields its sole prompt, flagged as the entry.
func enumerateAgents(packJSON string) ([]AgentInfo, error) {
	pack, err := decodePack(packJSON)
	if err != nil {
		return nil, err
	}

	names, entry := agentNames(pack)
	if len(names) == 0 {
		return nil, fmt.Errorf("pack has no prompts to deploy")
	}

	sort.Strings(names)
	agents := make([]AgentInfo, 0, len(names))
	for _, name := range names {
		agents = append(agents, AgentInfo{Name: name, IsEntry: name == entry})
	}
	return agents, nil
}

// agentNames returns the deployable agent names and which one is the entry.
func agentNames(pack *packView) (names []string, entry string) {
	if pack.Agents != nil && len(pack.Agents.Members) > 0 {
		for name := range pack.Agents.Members {
			names = append(names, name)
		}
		return names, pack.Agents.Entry
	}

	for name := range pack.Prompts {
		names = append(names, name)
	}
	if len(names) == 1 {
		return names, names[0]
	}
	return names, ""
}

// packID returns the pack's id, which seeds the promptkit-pack label.
func packID(packJSON string) (string, error) {
	pack, err := decodePack(packJSON)
	if err != nil {
		return "", err
	}
	if pack.ID == "" {
		return "", fmt.Errorf("pack has no id")
	}
	return pack.ID, nil
}

// hasA2ATools reports whether the pack declares agent-to-agent tools. Those
// calls cannot resolve until A2A wiring exists, so Plan warns about them.
// Malformed JSON reports false; enumerateAgents surfaces the parse error.
func hasA2ATools(packJSON string) bool {
	pack, err := decodePack(packJSON)
	if err != nil {
		return false
	}
	for name := range pack.Tools {
		if strings.HasPrefix(name, a2aToolPrefix) {
			return true
		}
	}
	return false
}
