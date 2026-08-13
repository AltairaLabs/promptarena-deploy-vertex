package vertex

import (
	"strings"
	"testing"
)

func TestParseState_Empty(t *testing.T) {
	s, err := parseState("")
	if err != nil {
		t.Fatalf("parseState: %v", err)
	}
	if s == nil {
		t.Fatal("expected a non-nil empty state")
	}
	if len(s.Engines) != 0 {
		t.Errorf("Engines = %v, want empty", s.Engines)
	}
	if s.Version != StateVersion {
		t.Errorf("Version = %d, want %d", s.Version, StateVersion)
	}
}

func TestParseState_Invalid(t *testing.T) {
	if _, err := parseState(`{not json`); err == nil {
		t.Fatal("expected an error for malformed state JSON")
	}
}

func TestStateRoundTrip(t *testing.T) {
	original := newState()
	original.AdapterVersion = "v1.2.3"
	original.PackHash = "abc123"
	original.ConfigHash = "def456"
	original.ImageDigest = "sha256:deadbeef"
	original.StagedPackURI = "gs://bucket/pack.json"
	original.StagedPackGeneration = 42
	original.Engines = map[string]EngineState{
		"assistant": {
			ResourceName: "projects/p/locations/us-central1/reasoningEngines/123",
		},
	}

	raw, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	got, err := parseState(raw)
	if err != nil {
		t.Fatalf("parseState: %v", err)
	}

	if got.AdapterVersion != "v1.2.3" {
		t.Errorf("AdapterVersion = %q", got.AdapterVersion)
	}
	if got.PackHash != "abc123" || got.ConfigHash != "def456" {
		t.Errorf("hashes = %q / %q", got.PackHash, got.ConfigHash)
	}
	if got.ImageDigest != "sha256:deadbeef" {
		t.Errorf("ImageDigest = %q", got.ImageDigest)
	}
	if got.StagedPackGeneration != 42 {
		t.Errorf("StagedPackGeneration = %d", got.StagedPackGeneration)
	}
	engine, ok := got.Engines["assistant"]
	if !ok {
		t.Fatal("engine entry lost in round trip")
	}
	if engine.ResourceName != "projects/p/locations/us-central1/reasoningEngines/123" {
		t.Errorf("ResourceName = %q", engine.ResourceName)
	}
}

func TestParseState_PreservesUnknownFields(t *testing.T) {
	raw := `{"version":1,"engines":{},"future_field":"keep me"}`

	s, err := parseState(raw)
	if err != nil {
		t.Fatalf("parseState: %v", err)
	}

	out, err := s.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(out, "future_field") {
		t.Errorf("unknown field dropped in round trip: %s", out)
	}
}

func TestParseState_RejectsNewerVersion(t *testing.T) {
	if _, err := parseState(`{"version":999,"engines":{}}`); err == nil {
		t.Fatal("expected an error for a state version from the future")
	}
}
