package vertex

import (
	"strings"
	"testing"
)

func TestEncodeToolSpecs_Empty(t *testing.T) {
	got, err := encodeToolSpecs(&ArenaConfig{})
	if err != nil {
		t.Fatalf("encodeToolSpecs: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty so the env var stays unset", got)
	}
}

func TestEncodeToolSpecs_NilArena(t *testing.T) {
	got, err := encodeToolSpecs(nil)
	if err != nil {
		t.Fatalf("encodeToolSpecs: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// The runtime, not the adapter, owns the tool spec schema. This asserts the
// adapter passes fields through untouched — including ones it does not model.
func TestEncodeToolSpecs_PreservesUnknownFields(t *testing.T) {
	arena, err := parseArenaConfig(`{"tool_specs":{"lookup_order":` +
		`{"mode":"mock","mock_result":{"status":"shipped"},"future_field":42}}}`)
	if err != nil {
		t.Fatalf("parseArenaConfig: %v", err)
	}

	got, err := encodeToolSpecs(arena)
	if err != nil {
		t.Fatalf("encodeToolSpecs: %v", err)
	}
	if !strings.Contains(got, `"future_field":42`) {
		t.Errorf("unknown field dropped; got %s", got)
	}
	if !strings.Contains(got, `"status":"shipped"`) {
		t.Errorf("mock_result dropped; got %s", got)
	}
}
