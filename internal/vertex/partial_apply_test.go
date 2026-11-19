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

	// beta and gamma still run the prior pack. Advancing the hashes would make
	// the next plan report them up to date.
	if got.PackHash != "old-pack" || got.ConfigHash != "old-config" {
		t.Errorf("hashes = %q/%q, want the prior old-pack/old-config", got.PackHash, got.ConfigHash)
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
