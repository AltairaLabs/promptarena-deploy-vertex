package vertex

import (
	"strings"
	"testing"
)

func TestDecidePackDelivery_Inline(t *testing.T) {
	cfg := &Config{PackInlineLimitBytes: 100}

	got := decidePackDelivery(`{"id":"small"}`, cfg)
	if !got.Inline {
		t.Error("a small pack should be delivered inline")
	}
	if got.SizeBytes != len(`{"id":"small"}`) {
		t.Errorf("SizeBytes = %d", got.SizeBytes)
	}
}

func TestDecidePackDelivery_Staged(t *testing.T) {
	cfg := &Config{PackInlineLimitBytes: 10}

	if decidePackDelivery(strings.Repeat("x", 50), cfg).Inline {
		t.Error("a pack over the limit should be staged")
	}
}

func TestDecidePackDelivery_ExactlyAtLimitIsInline(t *testing.T) {
	cfg := &Config{PackInlineLimitBytes: 5}

	if !decidePackDelivery("12345", cfg).Inline {
		t.Error("a pack exactly at the limit should still be inline")
	}
}

func TestDecidePackDelivery_ZeroLimitUsesDefault(t *testing.T) {
	cfg := &Config{}

	if !decidePackDelivery(`{"id":"small"}`, cfg).Inline {
		t.Error("an unset limit should fall back to the default, which is large")
	}
}

func TestEnumerateAgents_SinglePrompt(t *testing.T) {
	agents, err := enumerateAgents(`{"id":"p","prompts":{"solo":{}}}`)
	if err != nil {
		t.Fatalf("enumerateAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len = %d, want 1", len(agents))
	}
	if agents[0].Name != "solo" || !agents[0].IsEntry {
		t.Errorf("agent = %+v", agents[0])
	}
}

func TestEnumerateAgents_MultiAgentSortedWithEntryFlagged(t *testing.T) {
	agents, err := enumerateAgents(`{
		"id":"p",
		"prompts":{"beta":{},"alpha":{}},
		"agents":{"entry":"beta","members":{"alpha":{},"beta":{}}}
	}`)
	if err != nil {
		t.Fatalf("enumerateAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("len = %d, want 2", len(agents))
	}
	if agents[0].Name != "alpha" || agents[1].Name != "beta" {
		t.Errorf("agents are not sorted by name: %+v", agents)
	}
	if agents[0].IsEntry {
		t.Error("alpha should not be the entry agent")
	}
	if !agents[1].IsEntry {
		t.Error("beta should be the entry agent")
	}
}

func TestEnumerateAgents_InvalidJSON(t *testing.T) {
	if _, err := enumerateAgents(`{not json`); err == nil {
		t.Fatal("expected an error for malformed pack JSON")
	}
}

func TestEnumerateAgents_NoPrompts(t *testing.T) {
	if _, err := enumerateAgents(`{"id":"p"}`); err == nil {
		t.Fatal("expected an error for a pack with no prompts")
	}
}

func TestPackID(t *testing.T) {
	got, err := packID(`{"id":"my-pack"}`)
	if err != nil {
		t.Fatalf("packID: %v", err)
	}
	if got != "my-pack" {
		t.Errorf("got %q", got)
	}
}

func TestPackID_Missing(t *testing.T) {
	if _, err := packID(`{}`); err == nil {
		t.Fatal("expected an error when the pack has no id")
	}
}

func TestPackID_InvalidJSON(t *testing.T) {
	if _, err := packID(`{not json`); err == nil {
		t.Fatal("expected an error for malformed pack JSON")
	}
}

func TestHasA2ATools(t *testing.T) {
	with := `{"id":"p","tools":{"a2a__weather__forecast":{}}}`
	without := `{"id":"p","tools":{"get_weather":{}}}`

	if !hasA2ATools(with) {
		t.Error("expected a2a tools to be detected")
	}
	if hasA2ATools(without) {
		t.Error("no a2a tools should be detected")
	}
	if hasA2ATools(`{not json`) {
		t.Error("malformed JSON should not report a2a tools")
	}
}
