package vertex

import (
	"strings"
	"testing"
)

func TestParseArenaConfig_Empty(t *testing.T) {
	arena, err := parseArenaConfig("")
	if err != nil {
		t.Fatalf("parseArenaConfig: %v", err)
	}
	if arena != nil {
		t.Errorf("expected nil for empty arena config, got %+v", arena)
	}
}

func TestParseArenaConfig_Invalid(t *testing.T) {
	if _, err := parseArenaConfig(`{not json`); err == nil {
		t.Fatal("expected an error for malformed arena config")
	}
}

func TestParseArenaConfig_ProviderSpecsFallback(t *testing.T) {
	arena, err := parseArenaConfig(
		`{"provider_specs":{"alt":{"id":"alt","type":"openai","model":"gpt-x"}}}`)
	if err != nil {
		t.Fatalf("parseArenaConfig: %v", err)
	}

	p := arena.provider("alt")
	if p == nil {
		t.Fatal("provider_specs entry not found")
	}
	if p.Type != "openai" {
		t.Errorf("Type = %q", p.Type)
	}
}

func TestParseArenaConfig_LoadedProvidersWin(t *testing.T) {
	arena, err := parseArenaConfig(`{
		"loaded_providers":{"main":{"id":"main","type":"gemini","model":"loaded"}},
		"provider_specs":{"main":{"id":"main","type":"gemini","model":"spec"}}
	}`)
	if err != nil {
		t.Fatalf("parseArenaConfig: %v", err)
	}

	if got := arena.provider("main"); got.Model != "loaded" {
		t.Errorf("Model = %q, want the loaded_providers value", got.Model)
	}
}

func TestArenaProvider_NilReceiver(t *testing.T) {
	var arena *ArenaConfig
	if arena.provider("anything") != nil {
		t.Error("nil ArenaConfig should resolve no providers")
	}
}

func TestResolveBindings_Inline(t *testing.T) {
	bindings := []ProviderBinding{
		{Name: DefaultBindingName, Role: RoleLLM, Type: "claude", Model: "claude-sonnet-4"},
	}

	resolved, errs := resolveBindings(bindings, nil)
	if len(errs) != 0 {
		t.Fatalf("resolveBindings: %v", errs)
	}
	if len(resolved) != 1 {
		t.Fatalf("len = %d, want 1", len(resolved))
	}
	if resolved[0].Type != "claude" || resolved[0].Model != "claude-sonnet-4" {
		t.Errorf("resolved = %+v", resolved[0])
	}
}

func TestResolveBindings_DefaultsRoleToLLM(t *testing.T) {
	resolved, errs := resolveBindings(
		[]ProviderBinding{{Name: "a", Type: "claude", Model: "m"}}, nil)
	if len(errs) != 0 {
		t.Fatalf("resolveBindings: %v", errs)
	}
	if resolved[0].Role != RoleLLM {
		t.Errorf("Role = %q, want %q", resolved[0].Role, RoleLLM)
	}
}

func TestResolveBindings_FromArena(t *testing.T) {
	arena, err := parseArenaConfig(
		`{"loaded_providers":{"main":{"id":"main","type":"gemini","model":"gemini-2.5-flash"}}}`)
	if err != nil {
		t.Fatalf("parseArenaConfig: %v", err)
	}

	resolved, errs := resolveBindings(
		[]ProviderBinding{{Name: DefaultBindingName, ArenaProvider: "main"}}, arena)
	if len(errs) != 0 {
		t.Fatalf("resolveBindings: %v", errs)
	}
	if resolved[0].Type != "gemini" || resolved[0].Model != "gemini-2.5-flash" {
		t.Errorf("resolved = %+v", resolved[0])
	}
}

func TestResolveBindings_UnknownArenaProvider(t *testing.T) {
	arena, _ := parseArenaConfig(`{"loaded_providers":{}}`)

	_, errs := resolveBindings([]ProviderBinding{{Name: "a", ArenaProvider: "missing"}}, arena)
	if len(errs) == 0 {
		t.Fatal("expected an error for an unknown arena provider")
	}
	if !strings.Contains(strings.Join(errs, "; "), "missing") {
		t.Errorf("error should name the missing provider, got %v", errs)
	}
}

func TestResolveBindings_ArenaProviderWithoutArenaConfig(t *testing.T) {
	_, errs := resolveBindings([]ProviderBinding{{Name: "a", ArenaProvider: "main"}}, nil)
	if len(errs) == 0 {
		t.Fatal("expected an error when arena_provider is used with no arena config")
	}
}

func TestPrimaryBinding_PrefersDefault(t *testing.T) {
	resolved := []ResolvedBinding{
		{Name: "alt", Role: RoleLLM, Type: "gemini", Model: "m"},
		{Name: DefaultBindingName, Role: RoleLLM, Type: "claude", Model: "m"},
	}

	got, ok := primaryBinding(resolved)
	if !ok {
		t.Fatal("primaryBinding returned false")
	}
	if got.Name != DefaultBindingName {
		t.Errorf("Name = %q", got.Name)
	}
}

func TestPrimaryBinding_FirstLLMOtherwise(t *testing.T) {
	resolved := []ResolvedBinding{
		{Name: "embed", Role: RoleEmbedding, Type: "gemini", Model: "m"},
		{Name: "main", Role: RoleLLM, Type: "claude", Model: "m"},
	}

	got, ok := primaryBinding(resolved)
	if !ok {
		t.Fatal("primaryBinding returned false")
	}
	if got.Name != "main" {
		t.Errorf("Name = %q, want \"main\"", got.Name)
	}
}

func TestPrimaryBinding_NoLLM(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "embed", Role: RoleEmbedding, Type: "g", Model: "m"}}

	if _, ok := primaryBinding(resolved); ok {
		t.Fatal("expected false when no llm-role binding exists")
	}
}
