package main

import "testing"

func TestParseProviderBindings_Empty(t *testing.T) {
	got, err := parseProviderBindings("")
	if err != nil {
		t.Fatalf("parseProviderBindings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseProviderBindings_List(t *testing.T) {
	raw := `[{"name":"default","role":"llm","type":"claude","model":"claude-sonnet-4"}]`

	got, err := parseProviderBindings(raw)
	if err != nil {
		t.Fatalf("parseProviderBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "default" || got[0].Type != "claude" || got[0].Model != "claude-sonnet-4" {
		t.Errorf("binding = %+v", got[0])
	}
}

func TestParseProviderBindings_Invalid(t *testing.T) {
	if _, err := parseProviderBindings(`{"not":"a list"}`); err == nil {
		t.Fatal("expected error for non-list JSON, got nil")
	}
}

func TestPrimaryBinding_PrefersDefault(t *testing.T) {
	bindings := []providerBinding{
		{Name: "alt", Role: "llm", Type: "gemini", Model: "gemini-2.5-pro"},
		{Name: "default", Role: "llm", Type: "claude", Model: "claude-sonnet-4"},
	}

	got, ok := primaryBinding(bindings)
	if !ok {
		t.Fatal("primaryBinding returned false")
	}
	if got.Name != "default" {
		t.Errorf("Name = %q, want \"default\"", got.Name)
	}
}

func TestPrimaryBinding_FallsBackToFirstLLM(t *testing.T) {
	bindings := []providerBinding{
		{Name: "embed", Role: "embedding", Type: "gemini", Model: "text-embedding-004"},
		{Name: "main", Role: "llm", Type: "claude", Model: "claude-sonnet-4"},
	}

	got, ok := primaryBinding(bindings)
	if !ok {
		t.Fatal("primaryBinding returned false")
	}
	if got.Name != "main" {
		t.Errorf("Name = %q, want \"main\"", got.Name)
	}
}

func TestPrimaryBinding_NoLLM(t *testing.T) {
	bindings := []providerBinding{
		{Name: "embed", Role: "embedding", Type: "gemini", Model: "text-embedding-004"},
	}

	if _, ok := primaryBinding(bindings); ok {
		t.Fatal("expected false when no llm-role binding exists")
	}
}

func TestBuildSDKOptions_NoBindings(t *testing.T) {
	cfg := &runtimeConfig{Project: "p", Location: "us-central1"}

	opts, err := buildSDKOptions(cfg)
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	if len(opts) != 0 {
		t.Errorf("len(opts) = %d, want 0", len(opts))
	}
}

func TestBuildSDKOptions_RequiresProjectAndLocation(t *testing.T) {
	cfg := &runtimeConfig{
		ProvidersJSON: `[{"name":"default","role":"llm","type":"claude","model":"claude-sonnet-4"}]`,
	}

	if _, err := buildSDKOptions(cfg); err == nil {
		t.Fatal("expected error when project/location are unset, got nil")
	}
}

func TestBuildSDKOptions_ProducesVertexOption(t *testing.T) {
	cfg := &runtimeConfig{
		ProvidersJSON: `[{"name":"default","role":"llm","type":"claude","model":"claude-sonnet-4"}]`,
		Project:       "my-project",
		Location:      "us-central1",
	}

	opts, err := buildSDKOptions(cfg)
	if err != nil {
		t.Fatalf("buildSDKOptions: %v", err)
	}
	if len(opts) != 1 {
		t.Errorf("len(opts) = %d, want 1", len(opts))
	}
}
