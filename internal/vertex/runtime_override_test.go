package vertex

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/v2/deploy"
)

const overrideImage = "us-central1-docker.pkg.dev/p/runtimes/other:1.0.0"

func TestGetProviderInfo_AdvertisesRuntimeOverride(t *testing.T) {
	info, err := NewProvider().GetProviderInfo(context.Background())
	if err != nil {
		t.Fatalf("GetProviderInfo: %v", err)
	}
	if !slices.Contains(info.Capabilities, deploy.RuntimeOverrideCapability) {
		t.Errorf("Capabilities = %v, want %q", info.Capabilities, deploy.RuntimeOverrideCapability)
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(info.ConfigSchema), &schema); err != nil {
		t.Fatalf("ConfigSchema: %v", err)
	}
	for _, key := range []string{deploy.RuntimeOverrideConfigKey, "env", "project", "providers"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("ConfigSchema has no %q property", key)
		}
	}
}

// Every field Config decodes must be in the schema, which sets
// additionalProperties false: a schema-validating client would otherwise
// reject a config the adapter accepts. env was missing before this test.
func TestConfigSchema_CoversEveryConfigField(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(configSchema), &schema); err != nil {
		t.Fatalf("configSchema: %v", err)
	}
	typ := reflect.TypeOf(Config{})
	for i := 0; i < typ.NumField(); i++ {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		if _, ok := schema.Properties[name]; !ok {
			t.Errorf("Config field %q is not in the schema", name)
		}
	}
}

func TestParseConfig_RuntimeOverride(t *testing.T) {
	cfg, err := parseConfig(`{
		"project":"p","location":"us-central1",
		"runtime_override":{
			"image":"` + overrideImage + `",
			"env":{"MODE":"remote"},
			"secret_env":{"API_KEY":{"name":"api-key"}}
		}
	}`)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.image() != overrideImage {
		t.Errorf("image() = %q, want the override's", cfg.image())
	}
	if errs := cfg.validateStructure(); len(errs) != 0 {
		t.Errorf("validateStructure: %v; an override image satisfies prebuilt mode", errs)
	}
}

func TestConfigImage_FallsBackToImage(t *testing.T) {
	cfg := &Config{Image: "base", RuntimeOverride: &deploy.RuntimeOverride{Env: map[string]string{"A": "1"}}}
	if cfg.image() != "base" {
		t.Errorf("image() = %q, want the config image when the override names none", cfg.image())
	}
}

func TestValidateStructure_RuntimeOverrideRefusals(t *testing.T) {
	cfg := &Config{
		Project: "p", Location: "us-central1", ImageMode: ImageModePrebuilt, Image: "img",
		Env: map[string]string{"SHARED": "a", "SHARED_SECRET": "b"},
		RuntimeOverride: &deploy.RuntimeOverride{
			Command: []string{"/bin/other"},
			Args:    []string{"-x"},
			Env: map[string]string{
				"SHARED":               "c",
				"PROMPTPACK_PROVIDERS": "x",
				"GOOGLE_CLOUD_PROJECT": "x",
				envOTLPEndpoint:        "x",
			},
			SecretEnv: map[string]deploy.SecretRef{"SHARED_SECRET": {Name: "s"}},
		},
	}
	got := strings.Join(cfg.validateStructure(), "\n")
	for _, want := range []string{
		"runtime_override.command is not supported",
		"runtime_override.args is not supported",
		`runtime_override.env "PROMPTPACK_PROVIDERS" is reserved`,
		`runtime_override.env "GOOGLE_CLOUD_PROJECT" is reserved`,
		`runtime_override.env "OTEL_EXPORTER_OTLP_ENDPOINT" is reserved`,
		`runtime_override.env "SHARED" is also set in env`,
		`runtime_override.secret_env "SHARED_SECRET" is also set in env`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("errors do not include %q:\n%s", want, got)
		}
	}
}

func TestBuildEngine_RuntimeOverride(t *testing.T) {
	in := testEngineInput()
	in.Cfg.Env = map[string]string{"FROM_ENV": "1"}
	in.Cfg.RuntimeOverride = &deploy.RuntimeOverride{
		Image:     overrideImage,
		Env:       map[string]string{"MODE": "remote"},
		SecretEnv: map[string]deploy.SecretRef{"API_KEY": {Name: "api-key", Version: "2"}},
	}

	spec, errs := buildEngine(in, AgentInfo{Name: "assistant", IsEntry: true})
	if len(errs) != 0 {
		t.Fatalf("buildEngine: %v", errs)
	}
	if spec.ImageURI != overrideImage {
		t.Errorf("ImageURI = %q, want the override image", spec.ImageURI)
	}
	if spec.Env["MODE"] != "remote" || spec.Env["FROM_ENV"] != "1" {
		t.Errorf("Env = %v, want both the override's and env's variables", spec.Env)
	}
	// The handoff a replacement runtime depends on is still there.
	if spec.Env[deploy.EnvPackJSON] != in.PackJSON || spec.Env[deploy.EnvAgent] != "assistant" {
		t.Errorf("pack handoff missing: %s=%q %s=%q",
			deploy.EnvPackJSON, spec.Env[deploy.EnvPackJSON], deploy.EnvAgent, spec.Env[deploy.EnvAgent])
	}
	if ref := spec.SecretEnv["API_KEY"]; ref.Name != "api-key" || ref.Version != "2" {
		t.Errorf("SecretEnv[API_KEY] = %+v", ref)
	}
	if _, leaked := spec.Env["API_KEY"]; leaked {
		t.Error("a secret variable must not also appear as a plain variable")
	}
}

// The adapter's own handoff names must be the SDK's, or a replacement runtime
// looking for the SDK names would find no pack.
func TestHandoffNamesMatchTheSDK(t *testing.T) {
	if envPackJSON != deploy.EnvPackJSON || envPackURI != deploy.EnvPackURI || envAgentName != deploy.EnvAgent {
		t.Errorf("adapter names %s/%s/%s differ from the SDK's %s/%s/%s",
			envPackJSON, envPackURI, envAgentName, deploy.EnvPackJSON, deploy.EnvPackURI, deploy.EnvAgent)
	}
}

func TestSpecToProto_SecretEnv(t *testing.T) {
	got := specToProto(&EngineSpec{
		DisplayName: "a",
		SecretEnv: map[string]deploy.SecretRef{
			"B_KEY": {Name: "b-secret", Version: "7"},
			"A_KEY": {Name: "a-secret"},
		},
	})
	deployment := got.GetSpec().GetDeploymentSpec()
	if deployment == nil {
		t.Fatal("secret env alone must produce a deployment spec")
	}
	secrets := deployment.GetSecretEnv()
	if len(secrets) != 2 {
		t.Fatalf("len(secretEnv) = %d, want 2", len(secrets))
	}
	if secrets[0].GetName() != "A_KEY" || secrets[1].GetName() != "B_KEY" {
		t.Errorf("secret env is not sorted by name: %v", secrets)
	}
	if ref := secrets[0].GetSecretRef(); ref.GetSecret() != "a-secret" || ref.GetVersion() != "latest" {
		t.Errorf("A_KEY ref = %v, want a-secret at latest", ref)
	}
	if ref := secrets[1].GetSecretRef(); ref.GetSecret() != "b-secret" || ref.GetVersion() != "7" {
		t.Errorf("B_KEY ref = %v, want b-secret at version 7", ref)
	}
	if len(deployment.GetEnv()) != 0 {
		t.Errorf("Env = %v, want none", deployment.GetEnv())
	}
}

func TestHashPlanConfig_ChangesWithRuntimeOverrideAndEnv(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	base := &Config{Project: "p", Location: "l"}
	withImage := &Config{Project: "p", Location: "l",
		RuntimeOverride: &deploy.RuntimeOverride{Image: overrideImage}}
	withSecret := &Config{Project: "p", Location: "l",
		RuntimeOverride: &deploy.RuntimeOverride{Image: overrideImage,
			SecretEnv: map[string]deploy.SecretRef{"K": {Name: "s"}}}}
	withEnv := &Config{Project: "p", Location: "l", Env: map[string]string{"A": "1"}}

	hashes := map[string]string{}
	for name, cfg := range map[string]*Config{
		"base": base, "image": withImage, "secret": withSecret, "env": withEnv,
	} {
		h, err := hashPlanConfig(cfg, resolved, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for other, oh := range hashes {
			if oh == h {
				t.Errorf("%s and %s hash the same; each is a different deployed engine", name, other)
			}
		}
		hashes[name] = h
	}
}

// A config that sets neither env nor runtime_override must hash exactly as it
// did before those fields joined the fingerprint, or every existing deployment
// would plan a spurious update after the adapter upgrade.
func TestHashPlanConfig_UnsetNewFieldsKeepTheOldHash(t *testing.T) {
	cfg := &Config{Project: "p", Location: "l", Image: "img"}
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}

	got, err := hashPlanConfig(cfg, resolved, "")
	if err != nil {
		t.Fatalf("hashPlanConfig: %v", err)
	}
	// The fingerprint as it was serialized before env and runtime_override.
	old, err := json.Marshal(struct {
		Project              string            `json:"project"`
		Location             string            `json:"location"`
		Image                string            `json:"image"`
		ImageMode            string            `json:"image_mode"`
		ServiceAccount       string            `json:"service_account"`
		ResourceLimits       *ResourceLimits   `json:"resource_limits"`
		MinInstances         *int              `json:"min_instances"`
		MaxInstances         *int              `json:"max_instances"`
		ContainerConcurrency *int              `json:"container_concurrency"`
		Labels               map[string]string `json:"labels"`
		Bindings             []ResolvedBinding `json:"bindings"`
		Observability        *Observability    `json:"observability"`
		ToolSpecs            string            `json:"tool_specs"`
	}{Project: "p", Location: "l", Image: "img", Bindings: resolved})
	if err != nil {
		t.Fatal(err)
	}
	if want := hashString(string(old)); got != want {
		t.Errorf("hash = %s, want the pre-upgrade hash %s", got, want)
	}
}

// End to end through the provider with the dry-run client: a config with an
// override validates, plans and applies without touching GCP.
func TestProviderApply_DryRunWithRuntimeOverride(t *testing.T) {
	cfg := `{
		"project":"p","location":"us-central1","dry_run":true,
		"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}],
		"runtime_override":{
			"image":"` + overrideImage + `",
			"env":{"MODE":"remote"},
			"secret_env":{"API_KEY":{"name":"api-key"}}
		}
	}`
	p := NewProvider()
	resp, err := p.ValidateConfig(context.Background(), &deploy.ValidateRequest{Config: cfg})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if !resp.Valid {
		t.Fatalf("ValidateConfig errors: %v", resp.Errors)
	}

	req := &deploy.PlanRequest{PackJSON: `{"id":"demo","prompts":{"assistant":{}}}`, DeployConfig: cfg}
	state, err := p.Apply(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	parsed, err := parseState(state)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if _, ok := parsed.Engines["assistant"]; !ok {
		t.Errorf("state has no engine for assistant: %+v", parsed.Engines)
	}
}

func TestValidateConfig_RefusesCommandOverride(t *testing.T) {
	resp, err := NewProvider().ValidateConfig(context.Background(), &deploy.ValidateRequest{Config: `{
		"project":"p","location":"us-central1","image":"img",
		"providers":[{"name":"default","role":"llm","type":"gemini","model":"m"}],
		"runtime_override":{"command":["/bin/other"]}
	}`})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if resp.Valid {
		t.Fatal("a command override was accepted, though Agent Runtime cannot apply it")
	}
	if !strings.Contains(strings.Join(resp.Errors, "\n"), "runtime_override.command") {
		t.Errorf("Errors = %v", resp.Errors)
	}
}

// Configs that deploy the same engine must hash the same, or each costs a
// spurious update.
func TestParseConfig_NormalizesRuntimeOverride(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	hashOf := func(raw string) string {
		t.Helper()
		cfg, err := parseConfig(raw)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		h, err := hashPlanConfig(cfg, resolved, "")
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if hashOf(`{"project":"p","location":"l"}`) != hashOf(`{"project":"p","location":"l","runtime_override":{}}`) {
		t.Error("an empty runtime_override hashes differently from none")
	}
	unversioned := `{"project":"p","location":"l","runtime_override":{"secret_env":{"K":{"name":"s"}}}}`
	latest := `{"project":"p","location":"l","runtime_override":{"secret_env":{"K":{"name":"s","version":"latest"}}}}`
	if hashOf(unversioned) != hashOf(latest) {
		t.Error("a secret with no version hashes differently from the same secret at latest")
	}
}
