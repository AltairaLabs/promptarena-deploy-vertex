package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
)

// failingClient wraps the dry-run client and fails one named agent's create, so
// partial-failure behavior is testable without GCP.
type failingClient struct {
	*dryRunClient
	failOn string
}

func (c *failingClient) CreateEngine(ctx context.Context, spec *EngineSpec) (*Engine, error) {
	if spec.DisplayName == c.failOn {
		return nil, errors.New("simulated create failure")
	}
	return c.dryRunClient.CreateEngine(ctx, spec)
}

func testApplyInput() *engineInput {
	return &engineInput{
		Cfg: &Config{
			Project:  "p",
			Location: "us-central1",
			Image:    "us-central1-docker.pkg.dev/p/r/i",
		},
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		PackID:   "demo",
		Bindings: []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}},
		Delivery: PackDelivery{Inline: true, SizeBytes: 40},
	}
}

func TestApplyEngines_CreatesAndRecordsState(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	agents := []AgentInfo{{Name: "assistant", IsEntry: true}}

	got, err := applyEngines(context.Background(), client, testApplyInput(), agents, newState(), nil)
	if err != nil {
		t.Fatalf("applyEngines: %v", err)
	}

	engine, ok := got.Engines["assistant"]
	if !ok {
		t.Fatalf("state has no entry for assistant: %+v", got.Engines)
	}
	if engine.ResourceName == "" {
		t.Error("ResourceName was not recorded")
	}
	if engine.InFlight {
		t.Error("a completed create must not be marked in-flight")
	}
}

func TestApplyEngines_UpdatesExisting(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	agents := []AgentInfo{{Name: "assistant"}}

	first, err := applyEngines(context.Background(), client, testApplyInput(), agents, newState(), nil)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}

	second, err := applyEngines(context.Background(), client, testApplyInput(), agents, first, nil)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if second.Engines["assistant"].ResourceName != first.Engines["assistant"].ResourceName {
		t.Error("re-apply should update in place, not create a new engine")
	}
}

func TestApplyEngines_RecreatesWhenPriorEngineIsGone(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	agents := []AgentInfo{{Name: "assistant"}}

	stale := "projects/p/locations/us-central1/reasoningEngines/stale"
	prior := newState()
	prior.Engines = map[string]EngineState{"assistant": {ResourceName: stale}}

	got, err := applyEngines(context.Background(), client, testApplyInput(), agents, prior, nil)
	if err != nil {
		t.Fatalf("applyEngines: %v", err)
	}
	if got.Engines["assistant"].ResourceName == stale {
		t.Error("a vanished engine should be recreated, not left pointing at the stale name")
	}
}

func TestApplyEngines_PartialFailureKeepsSucceededEngines(t *testing.T) {
	client := &failingClient{
		dryRunClient: newDryRunClient(&Config{Project: "p", Location: "us-central1"}),
		failOn:       "beta",
	}
	agents := []AgentInfo{{Name: "alpha"}, {Name: "beta"}}

	got, err := applyEngines(context.Background(), client, testApplyInput(), agents, newState(), nil)
	if err == nil {
		t.Fatal("expected an error when one engine fails")
	}
	if got == nil {
		t.Fatal("state must be returned even on partial failure")
	}
	if _, ok := got.Engines["alpha"]; !ok {
		t.Error("the engine that succeeded must be recorded so it is not orphaned")
	}
	if _, ok := got.Engines["beta"]; ok {
		t.Error("the engine that failed must not be recorded as deployed")
	}
}

func TestApplyEngines_DeletesRemovedAgents(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})

	both := []AgentInfo{{Name: "alpha"}, {Name: "retired"}}
	afterFirst, err := applyEngines(context.Background(), client, testApplyInput(), both, newState(), nil)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}

	only := []AgentInfo{{Name: "alpha"}}
	got, err := applyEngines(context.Background(), client, testApplyInput(), only, afterFirst, nil)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if _, ok := got.Engines["retired"]; ok {
		t.Error("an agent removed from the pack should be deleted from state")
	}
}

func TestApplyEngines_RecordsHashes(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	in := testApplyInput()
	in.PackHash = "packhash"
	in.ConfigHash = "confighash"

	got, err := applyEngines(context.Background(), client, in,
		[]AgentInfo{{Name: "assistant"}}, newState(), nil)
	if err != nil {
		t.Fatalf("applyEngines: %v", err)
	}
	if got.PackHash != "packhash" || got.ConfigHash != "confighash" {
		t.Errorf("hashes = %q / %q", got.PackHash, got.ConfigHash)
	}
	if got.AdapterVersion != Version {
		t.Errorf("AdapterVersion = %q, want %q", got.AdapterVersion, Version)
	}
}

func TestProviderApply_DryRunMakesNoRealCalls(t *testing.T) {
	req := &deploy.PlanRequest{
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		DeployConfig: `{
			"project":"p","location":"us-central1","dry_run":true,
			"image":"us-central1-docker.pkg.dev/p/r/i",
			"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}]
		}`,
	}

	state, err := NewProvider().Apply(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state == "" {
		t.Fatal("Apply should return serialized state")
	}

	parsed, err := parseState(state)
	if err != nil {
		t.Fatalf("returned state does not parse: %v", err)
	}
	if _, ok := parsed.Engines["assistant"]; !ok {
		t.Errorf("dry-run state should record the simulated engine, got %+v", parsed.Engines)
	}
}

func TestProviderApply_StagedPackNotYetSupported(t *testing.T) {
	big := `{"id":"demo","prompts":{"assistant":{}},"padding":"` +
		strings.Repeat("x", 30000) + `"}`

	req := &deploy.PlanRequest{
		PackJSON: big,
		DeployConfig: `{
			"project":"p","location":"us-central1","dry_run":true,
			"staging_bucket":"gs://b",
			"image":"us-central1-docker.pkg.dev/p/r/i",
			"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}]
		}`,
	}

	if _, err := NewProvider().Apply(context.Background(), req, nil); err == nil {
		t.Fatal("a pack over the inline limit must fail until GCS staging exists")
	}
}

func TestProviderApply_RejectsInvalidConfig(t *testing.T) {
	req := &deploy.PlanRequest{
		PackJSON:     `{"id":"demo","prompts":{"assistant":{}}}`,
		DeployConfig: `{"location":"us-central1"}`,
	}

	if _, err := NewProvider().Apply(context.Background(), req, nil); err == nil {
		t.Fatal("a config missing project must fail before any API call")
	}
}
