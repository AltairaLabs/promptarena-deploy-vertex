package vertex

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/AltairaLabs/promptarena/v2/deploy"
)

// updateFailingClient fails the update of one named engine, so a failed apply
// can be made to stop partway through a pack.
type updateFailingClient struct {
	*dryRunClient
	failUpdateOf string
}

func (c *updateFailingClient) UpdateEngine(
	ctx context.Context, name string, spec *EngineSpec,
) (*Engine, error) {
	if name == c.failUpdateOf {
		return nil, errors.New("simulated update failure")
	}
	return c.dryRunClient.UpdateEngine(ctx, name, spec)
}

// creatingClient accepts a create but fails waiting for it, the way the real
// client reports an interrupted or timed-out long-running operation.
type creatingClient struct {
	*dryRunClient
}

func (c *creatingClient) CreateEngine(_ context.Context, spec *EngineSpec) (*Engine, error) {
	return nil, &engineCreatingError{
		ResourceName: "projects/p/locations/us-central1/reasoningEngines/" + spec.DisplayName,
		err:          context.DeadlineExceeded,
	}
}

func partialApplyProvider(client gcpClient) *Provider {
	p := NewProvider()
	p.clientFunc = func(context.Context, *Config) (gcpClient, error) { return client, nil }
	return p
}

func partialApplyRequest(packJSON, prior string) *deploy.PlanRequest {
	return &deploy.PlanRequest{
		PackJSON: packJSON,
		DeployConfig: `{
			"project":"p","location":"us-central1",
			"image":"us-central1-docker.pkg.dev/p/r/i",
			"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}]
		}`,
		PriorState: prior,
	}
}

// A failed apply that changed nothing hands the stored state back verbatim, so
// the CLI does not record an incomplete apply.
func TestFailedApplyThatChangedNothingReturnsPriorVerbatim(t *testing.T) {
	const prior = `{"version":1,"adapter_version":"old","engines":{}}`
	client := &failingClient{
		dryRunClient: newDryRunClient(&Config{Project: "p", Location: "us-central1"}),
		failOn:       "alpha",
	}

	raw, err := partialApplyProvider(client).Apply(context.Background(),
		partialApplyRequest(`{"id":"demo","prompts":{"alpha":{},"beta":{}}}`, prior), nil)
	if err == nil {
		t.Fatal("Apply succeeded, want alpha's create failure")
	}
	if raw != prior {
		t.Errorf("state = %s, want the prior state verbatim", raw)
	}
}

// A create the provider accepted but that did not finish may still produce an
// engine, so it is recorded as in-flight under its name.
func TestFailedApplyRecordsAnInterruptedCreateAsInFlight(t *testing.T) {
	client := &creatingClient{newDryRunClient(&Config{Project: "p", Location: "us-central1"})}

	raw, err := partialApplyProvider(client).Apply(context.Background(),
		partialApplyRequest(`{"id":"demo","prompts":{"alpha":{}}}`, ""), nil)
	if err == nil {
		t.Fatal("Apply succeeded, want the interrupted create")
	}

	got, parseErr := parseState(raw)
	if parseErr != nil {
		t.Fatalf("parseState: %v", parseErr)
	}
	want := EngineState{ResourceName: "projects/p/locations/us-central1/reasoningEngines/alpha", InFlight: true}
	if got.Engines["alpha"] != want {
		t.Errorf("alpha = %+v, want %+v", got.Engines["alpha"], want)
	}
}

// After a failed apply updated one engine, reverting to the prior pack must
// still plan an update for it: it runs the pack that was reverted away from.
func TestPlanAfterAFailedApplyUpdatesEveryEngineOnRevert(t *testing.T) {
	const oldPack = `{"id":"demo","prompts":{"alpha":{},"beta":{}},"v":1}`
	const newPack = `{"id":"demo","prompts":{"alpha":{},"beta":{}},"v":2}`
	dry := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	p := partialApplyProvider(dry)

	deployed, err := p.Apply(context.Background(), partialApplyRequest(oldPack, ""), nil)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	prior, _ := parseState(deployed)

	failing := &updateFailingClient{dryRunClient: dry, failUpdateOf: prior.Engines["beta"].ResourceName}
	partial, err := partialApplyProvider(failing).Apply(context.Background(), partialApplyRequest(newPack, deployed), nil)
	if err == nil {
		t.Fatal("second Apply succeeded, want beta's update failure")
	}
	state, _ := parseState(partial)

	agents, _ := enumerateAgents(oldPack)
	plan := buildPlan(&planInput{
		Agents: agents, Prior: state,
		PackHash: hashPack(oldPack), ConfigHash: prior.ConfigHash,
		Delivery: PackDelivery{Inline: true},
	})
	for _, c := range plan.Changes {
		if c.Action != deploy.ActionUpdate {
			t.Errorf("%s: action = %s, want update", c.Name, c.Action)
		}
	}
}

func TestEngineNameFromOperation(t *testing.T) {
	tests := map[string]string{
		"projects/p/locations/l/reasoningEngines/123/operations/456": "projects/p/locations/l/reasoningEngines/123",
		"projects/p/locations/l/operations/456":                      "",
		"projects/p/locations/l/reasoningEngines//operations/456":    "",
		"projects/p/locations/l/reasoningEngines/123":                "",
		"": "",
	}
	for op, want := range tests {
		if got := engineNameFromOperation(op); got != want {
			t.Errorf("engineNameFromOperation(%q) = %q, want %q", op, got, want)
		}
	}
}

func TestGetProviderInfoAdvertisesPartialApplyState(t *testing.T) {
	info, err := NewProvider().GetProviderInfo(context.Background())
	if err != nil {
		t.Fatalf("GetProviderInfo: %v", err)
	}
	if !slices.Contains(info.Capabilities, deploy.PartialApplyStateCapability) {
		t.Errorf("Capabilities = %v, want %q", info.Capabilities, deploy.PartialApplyStateCapability)
	}
}

// The CLI saves the state a failed apply returns in place of the stored one,
// because the adapter advertises partial_apply_state. That state must still
// describe every engine the prior state did, plus the ones the apply created,
// or the CLI forgets engines that exist and the next apply duplicates them.
func TestFailedApplyReturnsPriorEnginesAndCreatedOnes(t *testing.T) {
	const (
		betaEngine    = "projects/p/locations/us-central1/reasoningEngines/beta"
		gammaEngine   = "projects/p/locations/us-central1/reasoningEngines/gamma"
		retiredEngine = "projects/p/locations/us-central1/reasoningEngines/retired"
	)
	// alpha is new, beta's update fails, gamma comes after the failure, and
	// retired has left the pack but is deleted only after every agent applies.
	prior := `{"version":1,"pack_hash":"old-pack","config_hash":"old-config",` +
		`"staged_pack_generation":7,"future_field":"kept","engines":{` +
		`"beta":{"resource_name":"` + betaEngine + `"},` +
		`"gamma":{"resource_name":"` + gammaEngine + `"},` +
		`"retired":{"resource_name":"` + retiredEngine + `"}}}`

	client := &updateFailingClient{
		dryRunClient: newDryRunClient(&Config{Project: "p", Location: "us-central1"}),
		failUpdateOf: betaEngine,
	}
	p := NewProvider()
	p.clientFunc = func(context.Context, *Config) (gcpClient, error) { return client, nil }

	req := &deploy.PlanRequest{
		PackJSON: `{"id":"demo","prompts":{"alpha":{},"beta":{},"gamma":{}}}`,
		DeployConfig: `{
			"project":"p","location":"us-central1",
			"image":"us-central1-docker.pkg.dev/p/r/i",
			"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}]
		}`,
		PriorState: prior,
	}

	raw, err := p.Apply(context.Background(), req, nil)
	if err == nil {
		t.Fatal("Apply succeeded, want beta's update failure")
	}

	got, parseErr := parseState(raw)
	if parseErr != nil {
		t.Fatalf("parseState: %v", parseErr)
	}

	if got.Engines["alpha"].ResourceName == "" {
		t.Error("alpha was created by the failed apply but is not recorded")
	}
	for name, want := range map[string]string{
		"beta":    betaEngine,
		"gamma":   gammaEngine,
		"retired": retiredEngine,
	} {
		if got.Engines[name].ResourceName != want {
			t.Errorf("engine %s = %q, want the prior %q kept", name, got.Engines[name].ResourceName, want)
		}
	}

	// alpha runs the new pack and gamma the old one, so no hash describes them.
	// The new hash would hide gamma; the prior one would hide alpha if the
	// author reverted. Cleared, the next plan updates every engine.
	if got.PackHash != "" || got.ConfigHash != "" {
		t.Errorf("hashes = %q/%q, want both cleared", got.PackHash, got.ConfigHash)
	}
	if got.StagedPackGeneration != 7 {
		t.Errorf("StagedPackGeneration = %d, want the prior 7", got.StagedPackGeneration)
	}

	var fields map[string]json.RawMessage
	if jsonErr := json.Unmarshal([]byte(raw), &fields); jsonErr != nil {
		t.Fatalf("state is not a JSON object: %v", jsonErr)
	}
	if string(fields["future_field"]) != `"kept"` {
		t.Errorf("future_field = %s, want the prior state's unknown field kept", fields["future_field"])
	}
}

// Building the result from the prior state must not write through to it.
func TestApplyEnginesDoesNotMutatePriorState(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})
	prior := newState()
	prior.Engines["retired"] = EngineState{ResourceName: "projects/p/locations/us-central1/reasoningEngines/retired"}

	if _, err := applyEngines(context.Background(), client, testApplyInput(),
		[]AgentInfo{{Name: "assistant"}}, prior, nil); err != nil {
		t.Fatalf("applyEngines: %v", err)
	}

	if _, ok := prior.Engines["assistant"]; ok {
		t.Error("prior state gained the created engine")
	}
	if _, ok := prior.Engines["retired"]; !ok {
		t.Error("prior state lost the deleted engine")
	}
}
