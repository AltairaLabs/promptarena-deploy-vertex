package vertex

import (
	"encoding/json"
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

// The arena loader puts `tools: - file: …` manifests in loaded_tools and leaves
// tool_specs empty. Reading only tool_specs deployed an agent whose tools could
// never run — and every example in the promptarena repo uses the file form.
func TestEncodeToolSpecs_LoadedToolsFromYAML(t *testing.T) {
	manifest := []byte(`apiVersion: promptkit.altairalabs.ai/v1alpha1
kind: Tool
metadata:
  name: get_weather
spec:
  name: get_weather
  description: Get the current weather for a location
  mode: mock
  mock_result: "Sunny, 72F"
  timeout_ms: 5000
`)

	got, err := encodeToolSpecs(&ArenaConfig{
		LoadedTools: []ArenaToolData{{FilePath: "tools/get-weather.tool.yaml", Data: manifest}},
	})
	if err != nil {
		t.Fatalf("encodeToolSpecs: %v", err)
	}
	if got == "" {
		t.Fatal("a file-declared tool produced no specs; the deployed agent could not run it")
	}
	if !strings.Contains(got, `"mode":"mock"`) {
		t.Errorf("mode not carried through: %s", got)
	}
	if !strings.Contains(got, `"mock_result":"Sunny, 72F"`) {
		t.Errorf("mock_result not carried through: %s", got)
	}
	if !strings.Contains(got, `"get_weather"`) {
		t.Errorf("tool not keyed by name: %s", got)
	}
}

func TestEncodeToolSpecs_InlineSpecWinsOverLoadedCopy(t *testing.T) {
	// The loader copies inline tool_specs into loaded_tools as well, so the same
	// tool arrives twice. The JSON form must win.
	manifest := []byte("kind: Tool\nspec:\n  name: t\n  mode: mock\n  mock_result: \"from-yaml\"\n")

	got, err := encodeToolSpecs(&ArenaConfig{
		ToolSpecs:   map[string]json.RawMessage{"t": json.RawMessage(`{"mode":"mock","mock_result":"from-json"}`)},
		LoadedTools: []ArenaToolData{{Data: manifest}},
	})
	if err != nil {
		t.Fatalf("encodeToolSpecs: %v", err)
	}
	if !strings.Contains(got, "from-json") || strings.Contains(got, "from-yaml") {
		t.Errorf("inline tool_specs should win over the loaded copy; got %s", got)
	}
}

func TestEncodeToolSpecs_MalformedManifestIsSkipped(t *testing.T) {
	got, err := encodeToolSpecs(&ArenaConfig{
		LoadedTools: []ArenaToolData{
			{FilePath: "broken.yaml", Data: []byte("\tnot: [valid")},
			{Data: []byte("kind: Tool\nspec:\n  name: good\n  mode: mock\n  mock_result: \"ok\"\n")},
		},
	})
	if err != nil {
		t.Fatalf("one malformed tool file must not fail the deploy: %v", err)
	}
	if !strings.Contains(got, `"good"`) {
		t.Errorf("the valid tool should still be carried; got %s", got)
	}
}
