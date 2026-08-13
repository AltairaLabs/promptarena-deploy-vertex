package vertex

import (
	"encoding/json"
	"strings"
	"testing"
)

func testEngineInput() *engineInput {
	return &engineInput{
		Cfg: &Config{
			Project:  "my-project",
			Location: "us-central1",
			Image:    "us-central1-docker.pkg.dev/my-project/r/runtime:v1",
		},
		PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`,
		PackID:   "demo",
		Bindings: []ResolvedBinding{
			{Name: "default", Role: RoleLLM, Type: "gemini", Model: "gemini-2.5-flash"},
		},
		Delivery: PackDelivery{Inline: true, SizeBytes: 40},
	}
}

func TestBuildEngine_SetsImageAndDisplayName(t *testing.T) {
	spec, errs := buildEngine(testEngineInput(), AgentInfo{Name: "assistant", IsEntry: true})
	if len(errs) != 0 {
		t.Fatalf("buildEngine: %v", errs)
	}
	if spec.ImageURI != "us-central1-docker.pkg.dev/my-project/r/runtime:v1" {
		t.Errorf("ImageURI = %q", spec.ImageURI)
	}
	if spec.DisplayName != "assistant" {
		t.Errorf("DisplayName = %q", spec.DisplayName)
	}
}

func TestBuildEngine_InlinePackEnv(t *testing.T) {
	in := testEngineInput()

	spec, _ := buildEngine(in, AgentInfo{Name: "assistant"})
	if spec.Env[envPackJSON] != in.PackJSON {
		t.Errorf("%s = %q", envPackJSON, spec.Env[envPackJSON])
	}
	if _, ok := spec.Env[envPackURI]; ok {
		t.Error("an inline pack must not set the pack URI")
	}
}

func TestBuildEngine_StagedPackEnv(t *testing.T) {
	in := testEngineInput()
	in.Delivery = PackDelivery{Inline: false, SizeBytes: 40000}
	in.StagedPackURI = "gs://bucket/packs/demo.json"

	spec, _ := buildEngine(in, AgentInfo{Name: "assistant"})
	if spec.Env[envPackURI] != "gs://bucket/packs/demo.json" {
		t.Errorf("%s = %q", envPackURI, spec.Env[envPackURI])
	}
	if _, ok := spec.Env[envPackJSON]; ok {
		t.Error("a staged pack must not also be inlined")
	}
}

func TestBuildEngine_StagedPackRequiresURI(t *testing.T) {
	in := testEngineInput()
	in.Delivery = PackDelivery{Inline: false, SizeBytes: 40000}

	if _, errs := buildEngine(in, AgentInfo{Name: "assistant"}); len(errs) == 0 {
		t.Fatal("a staged pack with no URI should error")
	}
}

func TestBuildEngine_ProjectAndLocationEnv(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	if spec.Env[envProject] != "my-project" {
		t.Errorf("%s = %q", envProject, spec.Env[envProject])
	}
	if spec.Env[envLocation] != "us-central1" {
		t.Errorf("%s = %q", envLocation, spec.Env[envLocation])
	}
}

func TestBuildEngine_AgentNameEnv(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	if spec.Env[envAgentName] != "assistant" {
		t.Errorf("%s = %q", envAgentName, spec.Env[envAgentName])
	}
}

func TestBuildEngine_ProvidersEnvIsJSONList(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	var decoded []ResolvedBinding
	if err := json.Unmarshal([]byte(spec.Env[envProviders]), &decoded); err != nil {
		t.Fatalf("%s is not a JSON list: %v", envProviders, err)
	}
	if len(decoded) != 1 || decoded[0].Model != "gemini-2.5-flash" {
		t.Errorf("decoded = %+v", decoded)
	}
}

func TestBuildEngine_ManagedLabels(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	if spec.Labels[LabelPack] != "demo" {
		t.Errorf("%s = %q", LabelPack, spec.Labels[LabelPack])
	}
	if spec.Labels[LabelAgent] != "assistant" {
		t.Errorf("%s = %q", LabelAgent, spec.Labels[LabelAgent])
	}
	if spec.Labels[LabelManagedBy] != ManagedByValue {
		t.Errorf("%s = %q", LabelManagedBy, spec.Labels[LabelManagedBy])
	}
}

func TestBuildEngine_ResourceLimitsMapping(t *testing.T) {
	in := testEngineInput()
	in.Cfg.ResourceLimits = &ResourceLimits{CPU: "2", Memory: "4Gi"}

	spec, _ := buildEngine(in, AgentInfo{Name: "assistant"})
	if spec.ResourceLimits["cpu"] != "2" || spec.ResourceLimits["memory"] != "4Gi" {
		t.Errorf("ResourceLimits = %v, want the cpu/memory map the API accepts",
			spec.ResourceLimits)
	}
}

func TestBuildEngine_NoResourceLimitsLeavesNil(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	if spec.ResourceLimits != nil {
		t.Errorf("unset resource limits should stay nil so the API default applies, got %v",
			spec.ResourceLimits)
	}
}

func TestBuildEngine_AgentCardAttached(t *testing.T) {
	in := testEngineInput()
	in.AgentCards = map[string]map[string]any{
		"assistant": {"name": "assistant", "version": "1.0.0"},
	}

	spec, _ := buildEngine(in, AgentInfo{Name: "assistant"})
	if spec.AgentCard == nil {
		t.Fatal("expected the agent card to be attached to the spec")
	}
	if spec.AgentCard["name"] != "assistant" {
		t.Errorf("AgentCard = %v", spec.AgentCard)
	}
}

func TestBuildEngine_NoAgentCardIsFine(t *testing.T) {
	spec, errs := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})
	if len(errs) != 0 {
		t.Fatalf("a missing agent card must not be an error: %v", errs)
	}
	if spec.AgentCard != nil {
		t.Errorf("AgentCard = %v, want nil", spec.AgentCard)
	}
}

func TestBuildEngine_DescriptionNamesThePack(t *testing.T) {
	spec, _ := buildEngine(testEngineInput(), AgentInfo{Name: "assistant"})

	if !strings.Contains(spec.Description, "demo") {
		t.Errorf("Description should name the pack, got %q", spec.Description)
	}
}
