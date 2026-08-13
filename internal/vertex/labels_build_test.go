package vertex

import "testing"

func TestBuildLabels_AddsManagedLabels(t *testing.T) {
	cfg := &Config{Labels: map[string]string{"team": "platform"}}

	labels, errs := buildLabels(cfg, "My.Pack", "assistant")
	if len(errs) != 0 {
		t.Fatalf("buildLabels: %v", errs)
	}
	if labels[LabelPack] != "my-pack" {
		t.Errorf("%s = %q, want \"my-pack\"", LabelPack, labels[LabelPack])
	}
	if labels[LabelAgent] != "assistant" {
		t.Errorf("%s = %q", LabelAgent, labels[LabelAgent])
	}
	if labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("%s = %q", LabelManagedBy, labels[LabelManagedBy])
	}
	if labels["team"] != "platform" {
		t.Errorf("user label lost: %v", labels)
	}
}

func TestBuildLabels_UserCannotOverrideManaged(t *testing.T) {
	cfg := &Config{Labels: map[string]string{LabelPack: "hijacked"}}

	labels, _ := buildLabels(cfg, "real-pack", "assistant")
	if labels[LabelPack] != "real-pack" {
		t.Errorf("%s = %q, managed labels must win", LabelPack, labels[LabelPack])
	}
}

func TestBuildLabels_SanitizesUserKeys(t *testing.T) {
	cfg := &Config{Labels: map[string]string{"My.Team": "Platform Eng"}}

	labels, errs := buildLabels(cfg, "p", "a")
	if len(errs) != 0 {
		t.Fatalf("buildLabels: %v", errs)
	}
	if labels["my-team"] != "platform-eng" {
		t.Errorf("expected sanitized key and value, got %v", labels)
	}
}

func TestBuildLabels_PropagatesValidationErrors(t *testing.T) {
	cfg := &Config{Labels: map[string]string{"My.Team": "a", "my-team": "b"}}

	if _, errs := buildLabels(cfg, "p", "a"); len(errs) == 0 {
		t.Fatal("colliding labels should produce errors")
	}
}
