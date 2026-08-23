package vertex

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
)

func testPlanInput() *planInput {
	return &planInput{
		Agents:     []AgentInfo{{Name: "assistant", IsEntry: true}},
		Prior:      newState(),
		PackHash:   "packhash",
		ConfigHash: "confighash",
		Delivery:   PackDelivery{Inline: true, SizeBytes: 100},
	}
}

func findChange(changes []deploy.ResourceChange, resType, name string) *deploy.ResourceChange {
	for i := range changes {
		if changes[i].Type == resType && changes[i].Name == name {
			return &changes[i]
		}
	}
	return nil
}

func TestBuildPlan_FirstDeployCreates(t *testing.T) {
	got := buildPlan(testPlanInput())

	change := findChange(got.Changes, ResTypeAgentRuntime, "assistant")
	if change == nil {
		t.Fatalf("no agent_runtime change for assistant: %+v", got.Changes)
	}
	if change.Action != deploy.ActionCreate {
		t.Errorf("Action = %q, want CREATE", change.Action)
	}
}

func TestBuildPlan_UnchangedWhenHashesMatch(t *testing.T) {
	in := testPlanInput()
	in.Prior.PackHash = in.PackHash
	in.Prior.ConfigHash = in.ConfigHash
	in.Prior.Engines = map[string]EngineState{
		"assistant": {ResourceName: "projects/p/locations/l/reasoningEngines/1"},
	}

	change := findChange(buildPlan(in).Changes, ResTypeAgentRuntime, "assistant")
	if change == nil {
		t.Fatal("expected a change entry for assistant")
	}
	if change.Action != deploy.ActionNoChange {
		t.Errorf("Action = %q, want NO_CHANGE", change.Action)
	}
}

func TestBuildPlan_PackChangeUpdates(t *testing.T) {
	in := testPlanInput()
	in.Prior.PackHash = "different"
	in.Prior.ConfigHash = in.ConfigHash
	in.Prior.Engines = map[string]EngineState{
		"assistant": {ResourceName: "projects/p/locations/l/reasoningEngines/1"},
	}

	change := findChange(buildPlan(in).Changes, ResTypeAgentRuntime, "assistant")
	if change.Action != deploy.ActionUpdate {
		t.Errorf("Action = %q, want UPDATE", change.Action)
	}
	if !strings.Contains(change.Detail, "pack") {
		t.Errorf("Detail should name the pack change, got %q", change.Detail)
	}
}

func TestBuildPlan_ConfigChangeUpdates(t *testing.T) {
	in := testPlanInput()
	in.Prior.PackHash = in.PackHash
	in.Prior.ConfigHash = "different"
	in.Prior.Engines = map[string]EngineState{
		"assistant": {ResourceName: "projects/p/locations/l/reasoningEngines/1"},
	}

	change := findChange(buildPlan(in).Changes, ResTypeAgentRuntime, "assistant")
	if change.Action != deploy.ActionUpdate {
		t.Errorf("Action = %q, want UPDATE", change.Action)
	}
	if !strings.Contains(change.Detail, "config") {
		t.Errorf("Detail should name the config change, got %q", change.Detail)
	}
}

func TestBuildPlan_RemovedAgentIsDeleted(t *testing.T) {
	in := testPlanInput()
	in.Prior.PackHash = in.PackHash
	in.Prior.ConfigHash = in.ConfigHash
	in.Prior.Engines = map[string]EngineState{
		"assistant": {ResourceName: "projects/p/locations/l/reasoningEngines/1"},
		"retired":   {ResourceName: "projects/p/locations/l/reasoningEngines/2"},
	}

	change := findChange(buildPlan(in).Changes, ResTypeAgentRuntime, "retired")
	if change == nil {
		t.Fatal("expected a DELETE entry for the removed agent")
	}
	if change.Action != deploy.ActionDelete {
		t.Errorf("Action = %q, want DELETE", change.Action)
	}
}

func TestBuildPlan_InFlightEngineUpdates(t *testing.T) {
	in := testPlanInput()
	in.Prior.PackHash = in.PackHash
	in.Prior.ConfigHash = in.ConfigHash
	in.Prior.Engines = map[string]EngineState{
		"assistant": {ResourceName: "projects/p/locations/l/reasoningEngines/1", InFlight: true},
	}

	change := findChange(buildPlan(in).Changes, ResTypeAgentRuntime, "assistant")
	if change.Action != deploy.ActionUpdate {
		t.Errorf("an in-flight engine must be reconciled, got %q", change.Action)
	}
}

func TestBuildPlan_StagedPackAddsResource(t *testing.T) {
	in := testPlanInput()
	in.Delivery = PackDelivery{Inline: false, SizeBytes: 40000}

	if findChange(buildPlan(in).Changes, ResTypePackObject, stagedPackName) == nil {
		t.Error("a staged pack should appear as a pack_object resource")
	}
}

func TestBuildPlan_InlinePackAddsNoResource(t *testing.T) {
	if findChange(buildPlan(testPlanInput()).Changes, ResTypePackObject, stagedPackName) != nil {
		t.Error("an inline pack should not create a pack_object resource")
	}
}

func TestBuildPlan_A2AWarning(t *testing.T) {
	in := testPlanInput()
	in.HasA2ATools = true

	warnings := strings.Join(buildPlan(in).Warnings, "; ")
	if !strings.Contains(warnings, "a2a__") {
		t.Errorf("expected an A2A delegation warning, got %q", warnings)
	}
}

func TestBuildPlan_MultiAgentWarning(t *testing.T) {
	in := testPlanInput()
	in.Agents = []AgentInfo{
		{Name: "alpha"},
		{Name: "beta", IsEntry: true},
	}

	warnings := strings.Join(buildPlan(in).Warnings, "; ")
	if !strings.Contains(warnings, "independent") {
		t.Errorf("expected a multi-agent warning, got %q", warnings)
	}
}

func TestBuildPlan_StagedPackWarnsAboutSize(t *testing.T) {
	in := testPlanInput()
	in.Delivery = PackDelivery{Inline: false, SizeBytes: 40000}

	if !strings.Contains(strings.Join(buildPlan(in).Warnings, "; "), "Cloud Storage") {
		t.Error("expected a warning explaining why the pack is staged")
	}
}

func TestSummarizeChanges(t *testing.T) {
	tests := []struct {
		name    string
		changes []deploy.ResourceChange
		want    string
	}{
		{"none", nil, "No changes"},
		{
			"mixed",
			[]deploy.ResourceChange{
				{Action: deploy.ActionCreate},
				{Action: deploy.ActionCreate},
				{Action: deploy.ActionUpdate},
				{Action: deploy.ActionDelete},
				{Action: deploy.ActionNoChange},
			},
			"2 to create, 1 to update, 1 to delete, 1 unchanged",
		},
		{
			"only unchanged",
			[]deploy.ResourceChange{{Action: deploy.ActionNoChange}},
			"1 unchanged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarizeChanges(tt.changes); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderPlan_EndToEnd(t *testing.T) {
	req := &deploy.PlanRequest{
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		DeployConfig: `{
			"project":"my-project",
			"location":"us-central1",
			"image":"us-central1-docker.pkg.dev/my-project/r/i",
			"providers":[{"name":"default","role":"llm","arena_provider":"main"}]
		}`,
		ArenaConfig: `{"loaded_providers":{"main":{"id":"main","type":"gemini","model":"gemini-2.5-flash"}}}`,
	}

	got, err := NewProvider().Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(got.Changes) != 1 {
		t.Fatalf("Changes = %+v, want exactly one", got.Changes)
	}
	if got.Changes[0].Action != deploy.ActionCreate {
		t.Errorf("Action = %q, want CREATE", got.Changes[0].Action)
	}
	if got.Summary != "1 to create" {
		t.Errorf("Summary = %q", got.Summary)
	}
}

func TestProviderPlan_RejectsUnknownArenaProvider(t *testing.T) {
	req := &deploy.PlanRequest{
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		DeployConfig: `{
			"project":"my-project",
			"location":"us-central1",
			"image":"us-central1-docker.pkg.dev/my-project/r/i",
			"providers":[{"name":"default","role":"llm","arena_provider":"nope"}]
		}`,
		ArenaConfig: `{"loaded_providers":{}}`,
	}

	if _, err := NewProvider().Plan(context.Background(), req); err == nil {
		t.Fatal("expected an error naming the missing arena provider")
	}
}
