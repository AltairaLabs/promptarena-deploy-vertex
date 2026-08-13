package vertex

import (
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/agentcard"
)

// buildAgentCards generates one A2A Agent Card per agent, keyed by agent name.
//
// Cards are returned as generic JSON objects because ReasoningEngineSpec.agentCard
// is a free-form object in the API schema; round-tripping PromptKit's typed card
// through JSON keeps this adapter agnostic to card schema growth.
//
// Only packs that declare an agents section produce cards — PromptKit's
// generator returns nil when pack.Agents is nil. That is not an error:
// spec.agentCard is optional, so a single-prompt pack deploys without A2A
// discovery. Opting into A2A means adding an agents section to the pack.
func buildAgentCards(packJSON string) (map[string]map[string]any, error) {
	var pack prompt.Pack
	if err := json.Unmarshal([]byte(packJSON), &pack); err != nil {
		return nil, fmt.Errorf("invalid pack JSON: %w", err)
	}

	cards := agentcard.GenerateAgentCards(&pack)
	out := make(map[string]map[string]any, len(cards))

	for name, card := range cards {
		encoded, err := json.Marshal(card)
		if err != nil {
			return nil, fmt.Errorf("marshal agent card for %q: %w", name, err)
		}
		var generic map[string]any
		if decodeErr := json.Unmarshal(encoded, &generic); decodeErr != nil {
			return nil, fmt.Errorf("decode agent card for %q: %w", name, decodeErr)
		}
		out[name] = generic
	}

	return out, nil
}
