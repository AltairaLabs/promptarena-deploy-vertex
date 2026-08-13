package vertex

import "testing"

// packWithAgents declares an agents section, which is what makes PromptKit
// generate A2A cards at all.
const packWithAgents = `{
	"id":"demo","version":"1.0.0",
	"template_engine":{"version":"v1","syntax":"{{variable}}"},
	"prompts":{"assistant":{"id":"assistant","name":"Assistant","version":"1.0.0",
		"system_template":"You are helpful.","description":"A helpful assistant"}},
	"agents":{"entry":"assistant","members":{"assistant":{"description":"A helpful assistant"}}}
}`

func TestBuildAgentCards_FromAgentsSection(t *testing.T) {
	cards, err := buildAgentCards(packWithAgents)
	if err != nil {
		t.Fatalf("buildAgentCards: %v", err)
	}

	card, ok := cards["assistant"]
	if !ok {
		t.Fatalf("no card for assistant, got keys %v", keysOf(cards))
	}
	if card["name"] == nil {
		t.Errorf("card has no name field: %v", card)
	}
	if card["version"] != "1.0.0" {
		t.Errorf("card version = %v, want the pack version", card["version"])
	}
}

// A pack with no agents section produces no cards. That is PromptKit's
// documented contract (agentcard.GenerateAgentCards returns nil when
// pack.Agents is nil), and it is not an error: spec.agentCard is optional, so a
// single-prompt pack simply deploys without A2A discovery.
func TestBuildAgentCards_NoAgentsSectionYieldsNoCards(t *testing.T) {
	cards, err := buildAgentCards(`{
		"id":"demo","version":"1.0.0",
		"template_engine":{"version":"v1","syntax":"{{variable}}"},
		"prompts":{"assistant":{"id":"assistant","name":"Assistant","version":"1.0.0",
			"system_template":"You are helpful."}}
	}`)
	if err != nil {
		t.Fatalf("a pack without an agents section must not error: %v", err)
	}
	if len(cards) != 0 {
		t.Errorf("expected no cards, got %v", keysOf(cards))
	}
}

func TestBuildAgentCards_InvalidPack(t *testing.T) {
	if _, err := buildAgentCards(`{not json`); err == nil {
		t.Fatal("expected an error for malformed pack JSON")
	}
}

// keysOf lists a map's keys for test failure messages.
func keysOf(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
