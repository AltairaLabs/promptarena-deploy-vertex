package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// recordingClient is a gcpClient that answers GetEngine from a fixed set and
// records what was asked, so tests can assert the provider was consulted.
type recordingClient struct {
	gcpClient // unused methods panic if called, which is what we want

	existing map[string]bool
	failWith error
	gets     []string
}

func (c *recordingClient) GetEngine(_ context.Context, name string) (*Engine, error) {
	c.gets = append(c.gets, name)
	if c.failWith != nil {
		return nil, c.failWith
	}
	if !c.existing[name] {
		return nil, ErrEngineNotFound
	}
	return &Engine{ResourceName: name, State: EngineStateActive}, nil
}

func stateWithEngines(names ...string) *State {
	s := newState()
	for _, n := range names {
		s.Engines[n] = EngineState{ResourceName: "projects/p/locations/l/reasoningEngines/" + n}
	}
	return s
}

// planReq returns a Plan request for one agent, with the given prior state.
func planReq(t *testing.T, prior *State, dryRun bool) *deploy.PlanRequest {
	t.Helper()
	raw := ""
	if prior != nil {
		encoded, err := prior.Marshal()
		if err != nil {
			t.Fatalf("marshal state: %v", err)
		}
		raw = encoded
	}
	dry := ""
	if dryRun {
		dry = `"dry_run":true,`
	}
	return &deploy.PlanRequest{
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		DeployConfig: `{
			"project":"my-project",
			"location":"us-central1",
			"image":"us-central1-docker.pkg.dev/my-project/r/i",` + dry + `
			"providers":[{"name":"default","role":"llm","arena_provider":"main"}]
		}`,
		ArenaConfig: `{"loaded_providers":{"main":{"id":"main","type":"gemini","model":"gemini-2.5-flash"}}}`,
		PriorState:  raw,
	}
}

// The case this exists for: someone deletes the engine in the console. Without
// verification Plan reports an update against an engine that is gone, and
// Apply then fails with ErrEngineNotFound.
func TestProviderPlan_DriftedEngineIsPlannedForCreation(t *testing.T) {
	prior := stateWithEngines("assistant")
	prior.PackHash = hashPack(`{"id":"demo","prompts":{"assistant":{}}}`)

	client := &recordingClient{} // knows about no engines: the engine is gone
	p := NewProvider()
	p.clientFunc = func(_ context.Context, _ *Config) (gcpClient, error) { return client, nil }

	got, err := p.Plan(context.Background(), planReq(t, prior, false))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Action != deploy.ActionCreate {
		t.Fatalf("expected a single CREATE for the drifted engine, got %+v", got.Changes)
	}
	var warned bool
	for _, w := range got.Warnings {
		if strings.Contains(w, "assistant") && strings.Contains(w, "no longer exists") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("expected a drift warning naming the engine, got %v", got.Warnings)
	}
	if len(client.gets) == 0 {
		t.Error("Plan should have verified prior state against the control plane")
	}
}

// Dry run is the offline mode. Its client knows about no engines, so verifying
// against it would report every engine as drifted.
func TestProviderPlan_DryRunDoesNotVerify(t *testing.T) {
	prior := stateWithEngines("assistant")
	client := &recordingClient{}
	p := NewProvider()
	p.clientFunc = func(_ context.Context, _ *Config) (gcpClient, error) { return client, nil }

	got, err := p.Plan(context.Background(), planReq(t, prior, true))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(client.gets) != 0 {
		t.Errorf("dry run must not contact the control plane, but looked up %v", client.gets)
	}
	for _, w := range got.Warnings {
		if strings.Contains(w, "no longer exists") {
			t.Errorf("dry run must not report drift, got %q", w)
		}
	}
}

// Verification is best effort: an unreachable control plane must not stop a
// plan that previously needed no connectivity at all.
func TestProviderPlan_FallsBackWhenClientUnavailable(t *testing.T) {
	prior := stateWithEngines("assistant")
	prior.PackHash = hashPack(`{"id":"demo","prompts":{"assistant":{}}}`)

	p := NewProvider()
	p.clientFunc = func(_ context.Context, _ *Config) (gcpClient, error) {
		return nil, errors.New("no credentials")
	}

	got, err := p.Plan(context.Background(), planReq(t, prior, false))
	if err != nil {
		t.Fatalf("Plan must not fail when the control plane is unreachable: %v", err)
	}
	for _, c := range got.Changes {
		if c.Action == deploy.ActionCreate {
			t.Error("with verification unavailable, stored state should stand rather than recreating")
		}
	}
}

func TestReconcilePriorState_DropsEnginesThatNoLongerExist(t *testing.T) {
	prior := stateWithEngines("kept", "gone")
	client := &recordingClient{existing: map[string]bool{
		"projects/p/locations/l/reasoningEngines/kept": true,
	}}

	got, drift := reconcilePriorState(context.Background(), client, prior)

	if _, ok := got.Engines["gone"]; ok {
		t.Error("an engine deleted out of band must be dropped from prior state")
	}
	if _, ok := got.Engines["kept"]; !ok {
		t.Error("an engine that still exists must be kept")
	}
	if len(drift) != 1 || !strings.Contains(drift[0], "gone") {
		t.Errorf("expected drift naming the deleted engine, got %v", drift)
	}
}

// A failed lookup is not evidence of absence. Dropping would plan a CREATE
// for an engine that exists, and apply would then collide with it.
func TestReconcilePriorState_KeepsEngineWhenLookupFails(t *testing.T) {
	prior := stateWithEngines("unknown")
	client := &recordingClient{failWith: errors.New("deadline exceeded")}

	got, drift := reconcilePriorState(context.Background(), client, prior)

	if _, ok := got.Engines["unknown"]; !ok {
		t.Error("an engine whose lookup failed must be kept")
	}
	if len(drift) != 0 {
		t.Errorf("a failed lookup is not drift, got %v", drift)
	}
}

// An engine recorded mid-creation has no usable resource name to look up yet;
// apply already reconciles it, so verification must leave it alone.
func TestReconcilePriorState_KeepsInFlightEngines(t *testing.T) {
	prior := newState()
	prior.Engines["starting"] = EngineState{InFlight: true}
	client := &recordingClient{}

	got, drift := reconcilePriorState(context.Background(), client, prior)

	if _, ok := got.Engines["starting"]; !ok {
		t.Error("an in-flight engine must be kept for apply to reconcile")
	}
	if len(drift) != 0 {
		t.Errorf("an in-flight engine is not drift, got %v", drift)
	}
	if len(client.gets) != 0 {
		t.Errorf("an in-flight engine has no resource name to look up, got %v", client.gets)
	}
}

func TestReconcilePriorState_EmptyStateIsNoOp(t *testing.T) {
	client := &recordingClient{}
	got, drift := reconcilePriorState(context.Background(), client, newState())
	if len(got.Engines) != 0 || len(drift) != 0 || len(client.gets) != 0 {
		t.Errorf("nothing to verify; got engines=%v drift=%v gets=%v",
			got.Engines, drift, client.gets)
	}
}

func TestReconcilePriorState_PreservesStateFields(t *testing.T) {
	prior := stateWithEngines("a")
	prior.PackHash = "ph"
	prior.ConfigHash = "ch"
	prior.ImageDigest = "sha256:abc"
	client := &recordingClient{existing: map[string]bool{
		"projects/p/locations/l/reasoningEngines/a": true,
	}}

	got, _ := reconcilePriorState(context.Background(), client, prior)
	if got.PackHash != "ph" || got.ConfigHash != "ch" || got.ImageDigest != "sha256:abc" {
		t.Errorf("state fields must survive reconcile, got %+v", got)
	}
}
