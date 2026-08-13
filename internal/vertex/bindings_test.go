package vertex

import (
	"strings"
	"testing"
)

func TestValidateBindings_RequiresAtLeastOne(t *testing.T) {
	if errs := validateBindings(nil); len(errs) == 0 {
		t.Fatal("expected an error for zero bindings")
	}
}

func TestValidateBindings_RejectsDuplicateNames(t *testing.T) {
	bindings := []ProviderBinding{
		{Name: "default", Type: "claude", Model: "m"},
		{Name: "default", Type: "gemini", Model: "m"},
	}

	if !strings.Contains(strings.Join(validateBindings(bindings), "; "), "duplicated") {
		t.Error("expected a duplicate-name error")
	}
}

func TestValidateBindings_RejectsUnknownRole(t *testing.T) {
	bindings := []ProviderBinding{{Name: "a", Role: "oracle", Type: "claude", Model: "m"}}

	if !strings.Contains(strings.Join(validateBindings(bindings), "; "), "role") {
		t.Error("expected an invalid-role error")
	}
}

func TestValidateBindings_RequiresExactlyOneSource(t *testing.T) {
	both := []ProviderBinding{
		{Name: "a", ArenaProvider: "p", Type: "claude", Model: "m"},
	}
	if !strings.Contains(strings.Join(validateBindings(both), "; "), "exactly one") {
		t.Error("expected an error when both arena_provider and type/model are set")
	}

	neither := []ProviderBinding{{Name: "a"}}
	if !strings.Contains(strings.Join(validateBindings(neither), "; "), "exactly one") {
		t.Error("expected an error when neither source is set")
	}
}

func TestValidateBindings_InlineRequiresModel(t *testing.T) {
	bindings := []ProviderBinding{{Name: "a", Type: "claude"}}

	if !strings.Contains(strings.Join(validateBindings(bindings), "; "), "model") {
		t.Error("expected an error when type is set without model")
	}
}

func TestValidateBindings_Valid(t *testing.T) {
	bindings := []ProviderBinding{
		{Name: DefaultBindingName, Role: RoleLLM, Type: "claude", Model: "m"},
		{Name: "embed", Role: "embedding", ArenaProvider: "arena-embed"},
	}

	if errs := validateBindings(bindings); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestBindingWarnings_NoDefaultNamesThePrimary(t *testing.T) {
	bindings := []ProviderBinding{
		{Name: "zeta", Role: RoleLLM, Type: "claude", Model: "m"},
		{Name: "alpha", Role: RoleLLM, Type: "gemini", Model: "m"},
	}

	warnings := bindingWarnings(bindings)
	if len(warnings) == 0 {
		t.Fatal("expected a warning when no binding is named default")
	}
	if !strings.Contains(warnings[0], "alpha") {
		t.Errorf("warning should name the lexicographically first binding, got %q", warnings[0])
	}
}

func TestBindingWarnings_SilentWhenDefaultPresent(t *testing.T) {
	bindings := []ProviderBinding{
		{Name: DefaultBindingName, Role: RoleLLM, Type: "claude", Model: "m"},
		{Name: "alpha", Role: RoleLLM, Type: "gemini", Model: "m"},
	}

	if warnings := bindingWarnings(bindings); len(warnings) != 0 {
		t.Errorf("expected no warnings, got %v", warnings)
	}
}

// Arena-reference resolution (parseArenaConfig, resolveBindings) lands in
// Phase 1b-ii alongside Plan, which is the first caller with an arena config
// available. Its tests move with it.
