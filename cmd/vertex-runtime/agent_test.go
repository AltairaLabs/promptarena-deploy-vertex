package main

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

func TestResolveAgentName_EnvWins(t *testing.T) {
	cfg := &runtimeConfig{AgentName: "explicit"}
	pack := &prompt.Pack{
		Agents: &prompt.AgentsConfig{Entry: "entry"},
	}

	got, err := resolveAgentName(cfg, pack)
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "explicit" {
		t.Errorf("got %q, want \"explicit\"", got)
	}
}

func TestResolveAgentName_PackEntry(t *testing.T) {
	cfg := &runtimeConfig{}
	pack := &prompt.Pack{
		Agents: &prompt.AgentsConfig{Entry: "entry"},
	}

	got, err := resolveAgentName(cfg, pack)
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "entry" {
		t.Errorf("got %q, want \"entry\"", got)
	}
}

func TestResolveAgentName_SinglePrompt(t *testing.T) {
	cfg := &runtimeConfig{}
	pack := &prompt.Pack{
		Prompts: map[string]*prompt.PackPrompt{"solo": {}},
	}

	got, err := resolveAgentName(cfg, pack)
	if err != nil {
		t.Fatalf("resolveAgentName: %v", err)
	}
	if got != "solo" {
		t.Errorf("got %q, want \"solo\"", got)
	}
}

func TestResolveAgentName_Ambiguous(t *testing.T) {
	cfg := &runtimeConfig{}
	pack := &prompt.Pack{
		Prompts: map[string]*prompt.PackPrompt{"a": {}, "b": {}},
	}

	if _, err := resolveAgentName(cfg, pack); err == nil {
		t.Fatal("expected error for ambiguous pack, got nil")
	}
}
