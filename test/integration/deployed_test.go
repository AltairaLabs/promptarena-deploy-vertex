//go:build integration

// Package integration holds tests that deploy to a real GCP project.
//
// They are excluded from normal builds by the integration build tag and skip
// unless VERTEX_TEST_PROJECT and VERTEX_TEST_IMAGE are set. Running them
// creates billable Agent Runtime engines; each test deletes what it created,
// including on failure.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"

	"github.com/AltairaLabs/promptarena-deploy-vertex/internal/vertex"
)

// Environment variables that gate and configure these tests.
const (
	envProject  = "VERTEX_TEST_PROJECT"
	envLocation = "VERTEX_TEST_LOCATION"
	envImage    = "VERTEX_TEST_IMAGE"
)

// defaultLocation is used when VERTEX_TEST_LOCATION is unset.
const defaultLocation = "us-central1"

// invokeTimeout bounds a single call to the deployed engine.
const invokeTimeout = 120 * time.Second

// cloudPlatformScope is the OAuth scope needed to call Vertex.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// locationIndex is the position of the location segment in a resource name of
// the form projects/{p}/locations/{l}/reasoningEngines/{id}.
const locationIndex = 3

// featurePack declares a tool, a validator and a template variable, so the
// deployed engine exercises more than a bare system prompt.
const featurePack = `{
  "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
  "id": "vertex-integration",
  "name": "Vertex Integration Pack",
  "version": "1.0.0",
  "template_engine": { "version": "v1", "syntax": "{{variable}}" },
  "prompts": {
    "support": {
      "id": "support",
      "name": "Support Agent",
      "version": "1.0.0",
      "system_template": "You are a terse support agent. Answer in one short sentence.",
      "tools": ["lookup_order"],
      "validators": [
        { "type": "length", "enabled": true, "params": { "max_characters": 4000 } }
      ]
    }
  },
  "tools": {
    "lookup_order": {
      "name": "lookup_order",
      "description": "Look up an order by its id",
      "parameters": {
        "type": "object",
        "properties": { "order_id": { "type": "string", "description": "The order id" } },
        "required": ["order_id"]
      }
    }
  }
}`

// testEnv holds the resolved configuration for a run.
type testEnv struct {
	Project  string
	Location string
	Image    string
}

// requireEnv skips the test unless the required variables are present.
func requireEnv(t *testing.T) testEnv {
	t.Helper()

	project := os.Getenv(envProject)
	image := os.Getenv(envImage)
	if project == "" || image == "" {
		t.Skipf("set %s and %s to run deployed integration tests", envProject, envImage)
	}

	location := os.Getenv(envLocation)
	if location == "" {
		location = defaultLocation
	}

	return testEnv{Project: project, Location: location, Image: image}
}

// deployConfig builds the adapter's deploy config JSON.
func deployConfig(t *testing.T, env testEnv) string {
	t.Helper()

	cfg := map[string]any{
		"project":  env.Project,
		"location": env.Location,
		"image":    env.Image,
		"providers": []map[string]any{
			{"name": "default", "role": "llm", "type": "gemini", "model": "gemini-2.5-flash"},
		},
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal deploy config: %v", err)
	}
	return string(encoded)
}

// deployedEngine applies the feature pack and returns the engine resource name,
// registering cleanup that destroys it even when the test fails.
func deployedEngine(t *testing.T, env testEnv) string {
	t.Helper()

	provider := vertex.NewProvider()
	req := &deploy.PlanRequest{
		PackJSON:     featurePack,
		DeployConfig: deployConfig(t, env),
	}

	state, err := provider.Apply(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name := engineNameFromState(t, state)

	t.Cleanup(func() {
		if delErr := deleteEngine(name); delErr != nil {
			t.Errorf("cleanup: delete engine %s: %v — DELETE IT MANUALLY", name, delErr)
		}
	})

	return name
}

// stateShape is the subset of adapter state these tests read.
type stateShape struct {
	Engines map[string]struct {
		ResourceName string `json:"resource_name"`
	} `json:"engines"`
}

// engineNameFromState pulls the deployed engine's resource name out of state.
func engineNameFromState(t *testing.T, state string) string {
	t.Helper()

	var parsed stateShape
	if err := json.Unmarshal([]byte(state), &parsed); err != nil {
		t.Fatalf("parse adapter state: %v", err)
	}
	engine, ok := parsed.Engines["support"]
	if !ok || engine.ResourceName == "" {
		t.Fatalf("state has no engine for support: %s", state)
	}
	return engine.ResourceName
}

// authedRequest issues an ADC-authorized request against the Vertex endpoint.
func authedRequest(method, url, body string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), invokeTimeout)
	defer cancel()

	source, err := google.DefaultTokenSource(ctx, cloudPlatformScope)
	if err != nil {
		return 0, "", fmt.Errorf("default token source: %w", err)
	}
	token, err := source.Token()
	if err != nil {
		return 0, "", fmt.Errorf("token: %w", err)
	}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, "", fmt.Errorf("new request: %w", err)
	}
	token.SetAuthHeader(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read body: %w", err)
	}
	return resp.StatusCode, string(out), nil
}

// engineURL builds a method URL for a deployed engine.
func engineURL(name, location, method string) string {
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s:%s",
		location, name, method)
}

// deleteEngine removes a deployed engine. A missing engine is not an error, so
// cleanup stays idempotent.
func deleteEngine(name string) error {
	parts := strings.Split(name, "/")
	if len(parts) <= locationIndex {
		return fmt.Errorf("malformed engine name %q", name)
	}
	location := parts[locationIndex]

	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s", location, name)
	status, body, err := authedRequest(http.MethodDelete, url, "")
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return fmt.Errorf("delete returned %d: %s", status, body)
	}
	return nil
}

func TestDeployed_UnaryQuery(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	status, body, err := authedRequest(http.MethodPost,
		engineURL(name, env.Location, "query"),
		`{"class_method":"query","input":{"message":"Say hello in three words."}}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	var got struct {
		Output string `json:"output"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &got); unmarshalErr != nil {
		t.Fatalf("unmarshal %q: %v", body, unmarshalErr)
	}
	if got.Output == "" {
		t.Errorf("empty output from the deployed engine: %s", body)
	}
}

func TestDeployed_StreamQuery(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	status, body, err := authedRequest(http.MethodPost,
		engineURL(name, env.Location, "streamQuery"),
		`{"class_method":"stream_query","input":{"message":"Count from one to five."}}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("streamQuery returned an empty body")
	}
}

func TestDeployed_MultiTurn(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	for i, msg := range []string{"What is 2+2?", "And what is 3+3?"} {
		status, body, err := authedRequest(http.MethodPost,
			engineURL(name, env.Location, "query"),
			fmt.Sprintf(`{"class_method":"query","input":{"message":%q}}`, msg))
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if status != http.StatusOK {
			t.Fatalf("turn %d: status = %d, body = %s", i, status, body)
		}
	}
}

func TestDeployed_ReapplyIsIdempotent(t *testing.T) {
	env := requireEnv(t)
	first := deployedEngine(t, env)

	provider := vertex.NewProvider()
	state, err := provider.Apply(context.Background(), &deploy.PlanRequest{
		PackJSON:     featurePack,
		DeployConfig: deployConfig(t, env),
		PriorState: fmt.Sprintf(
			`{"version":1,"engines":{"support":{"resource_name":%q}}}`, first),
	}, nil)
	if err != nil {
		t.Fatalf("re-apply: %v", err)
	}

	if second := engineNameFromState(t, state); second != first {
		t.Errorf("re-apply created a new engine %s, want in-place update of %s", second, first)
	}
}
