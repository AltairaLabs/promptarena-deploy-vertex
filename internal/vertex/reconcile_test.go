package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
	"github.com/AltairaLabs/promptarena/deploy/adaptersdk"
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
	// Two changes: the DRIFT that explains what happened, and the CREATE that
	// replaces it. Drift travels as a change so it is counted and rendered
	// like everything else rather than as loose warning prose.
	if len(got.Changes) != 2 {
		t.Fatalf("expected a DRIFT and a CREATE for the drifted engine, got %+v", got.Changes)
	}
	if got.Changes[0].Action != deploy.ActionDrift ||
		!strings.Contains(got.Changes[0].Detail, "no longer exists") {
		t.Errorf("expected a DRIFT change explaining the engine is gone, got %+v", got.Changes[0])
	}
	if got.Changes[0].Name != "assistant" {
		t.Errorf("drift change should name the engine, got %q", got.Changes[0].Name)
	}
	if got.Changes[1].Action != deploy.ActionCreate {
		t.Errorf("expected the drifted engine to be recreated, got %+v", got.Changes[1])
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
	for _, c := range got.Changes {
		if c.Action == deploy.ActionDrift {
			t.Errorf("dry run must not report drift, got %+v", c)
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
	if len(drift) != 1 {
		t.Fatalf("expected drift for exactly the deleted engine, got %+v", drift)
	}
	if drift[0].Name != "gone" || drift[0].Action != deploy.ActionDrift {
		t.Errorf("expected a DRIFT change naming %q, got %+v", "gone", drift[0])
	}
	if drift[0].Type != ResTypeAgentRuntime {
		t.Errorf("drift type = %q, want %q", drift[0].Type, ResTypeAgentRuntime)
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

// A plan made entirely of drift must not summarize as "No changes" — that is
// exactly the case an operator needs to see. The local summarizer counted
// DRIFT as an update, which produced "1 to update" for an engine that had
// vanished.
func TestSummarizeChanges_DriftHasItsOwnBucket(t *testing.T) {
	tests := []struct {
		name    string
		changes []deploy.ResourceChange
		want    string
	}{
		{
			name: "drift is reported alongside the create that replaces it",
			changes: []deploy.ResourceChange{
				{Action: deploy.ActionDrift}, {Action: deploy.ActionCreate},
			},
			want: "1 to create, 1 drifted",
		},
		{
			name:    "a plan of nothing but drift still says so",
			changes: []deploy.ResourceChange{{Action: deploy.ActionDrift}},
			want:    "1 drifted",
		},
		{
			name:    "an empty plan is still no changes",
			changes: nil,
			want:    "No changes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarizeChanges(tt.changes); got != tt.want {
				t.Errorf("summarizeChanges = %q, want %q", got, tt.want)
			}
		})
	}
}

// Only a confirmed NotFound is evidence of absence. Vertex returns
// InvalidArgument for a malformed resource name, and treating that as "gone"
// would drop a live engine and plan a create that collides at apply.
func TestEngineProbe_OnlyNotFoundMeansAbsent(t *testing.T) {
	ref := adaptersdk.ResourceRef{Name: "a", ID: "projects/p/locations/l/reasoningEngines/a"}

	t.Run("missing engine is confirmed absent", func(t *testing.T) {
		probe := engineProbe{client: &recordingClient{}}
		got, err := probe.Exists(context.Background(), ref)
		if err != nil {
			t.Fatalf("a confirmed absence is not an error: %v", err)
		}
		if got != adaptersdk.ExistsNo {
			t.Errorf("existence = %v, want ExistsNo", got)
		}
	})

	t.Run("present engine exists", func(t *testing.T) {
		probe := engineProbe{client: &recordingClient{existing: map[string]bool{ref.ID: true}}}
		got, err := probe.Exists(context.Background(), ref)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != adaptersdk.ExistsYes {
			t.Errorf("existence = %v, want ExistsYes", got)
		}
	})

	t.Run("any other failure is unknown, not absent", func(t *testing.T) {
		probe := engineProbe{client: &recordingClient{failWith: errors.New("InvalidArgument")}}
		got, err := probe.Exists(context.Background(), ref)
		if err == nil {
			t.Error("a failed lookup must surface the error so the SDK keeps the resource")
		}
		if got != adaptersdk.ExistsUnknown {
			t.Errorf("existence = %v, want ExistsUnknown", got)
		}
	})
}
