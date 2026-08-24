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

	"github.com/AltairaLabs/promptarena/deploy"

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

// mockOrderStatus is the value only the tool knows. The model cannot produce it
// from the prompt, so seeing it in the answer proves the runtime ran the tool
// rather than improvised an order status.
const mockOrderStatus = "delivered to a purple locker in Reykjavik"

// featureArena is the arena config the CLI would hand the adapter. The compiled
// pack carries only the tool's schema; its execution config lives here.
const featureArena = `{
  "tool_specs": {
    "lookup_order": {
      "name": "lookup_order",
      "mode": "mock",
      "mock_template": "{\"order_id\":\"{{.order_id}}\",\"status\":\"` +
	mockOrderStatus + `\"}"
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
	name, _ := deployedEngineWithState(t, env)
	return name
}

// deployedEngineWithState is deployedEngine plus the state blob, which Destroy
// and Status both take as input.
func deployedEngineWithState(t *testing.T, env testEnv) (name, state string) {
	t.Helper()

	provider := vertex.NewProvider()
	req := &deploy.PlanRequest{
		PackJSON:     featurePack,
		DeployConfig: deployConfig(t, env),
		ArenaConfig:  featureArena,
	}

	state, err := provider.Apply(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	name = engineNameFromState(t, state)

	// Deleting an engine that is already gone is not an error, so this is safe
	// even for the tests that destroy it themselves.
	t.Cleanup(func() {
		if delErr := deleteEngine(name); delErr != nil {
			t.Errorf("cleanup: delete engine %s: %v — DELETE IT MANUALLY", name, delErr)
		}
	})

	return name, state
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

	// force, because an engine that has served a conversation owns sessions
	// and the API refuses to delete a parent with children. Without it the
	// engines that were actually used are the ones cleanup leaves behind.
	url := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s?force=true",
		location, name)
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

// askDeployed sends one unary turn and returns the engine's output.
func askDeployed(t *testing.T, name, location, message string) string {
	t.Helper()
	return askDeployedSession(t, name, location, message, "")
}

// askDeployedSession asks within a named conversation. An empty session is a
// one-off turn, which is what every request was before sessions existed.
func askDeployedSession(t *testing.T, name, location, message, session string) string {
	t.Helper()

	input := fmt.Sprintf(`{"message":%q}`, message)
	if session != "" {
		input = fmt.Sprintf(`{"message":%q,"session_id":%q}`, message, session)
	}

	status, body, err := authedRequest(http.MethodPost,
		engineURL(name, location, "query"),
		fmt.Sprintf(`{"class_method":"query","input":%s}`, input))
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
	return got.Output
}

// TestDeployed_SequentialTurnsAreIndependent pins what a request without a
// session still does.
//
// Naming no session is the behaviour every request had before sessions
// existed, and callers relying on it should keep it: each turn stands alone.
// TestDeployed_SessionCarriesConversation covers the other half.
func TestDeployed_SequentialTurnsAreIndependent(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	first := askDeployed(t, name, env.Location,
		"Remember this number: 8675309. Just acknowledge it.")
	if first == "" {
		t.Fatal("empty output on the first turn")
	}
	t.Logf("turn 1: %s", first)

	second := askDeployed(t, name, env.Location,
		"What number did I just ask you to remember? "+
			"If you have no record of it, reply exactly: NO_MEMORY")
	t.Logf("turn 2: %s", second)

	if strings.Contains(second, "8675309") {
		t.Errorf("a turn with no session recalled an earlier one: %q", second)
	}
}

// TestDeployed_SessionCarriesConversation is the feature: two turns naming the
// same session share a conversation.
//
// The engine has always had the storage — Agent Runtime sessions hang off the
// reasoningEngine — and the runtime simply never used it, so multi-turn
// context was the caller's problem.
func TestDeployed_SessionCarriesConversation(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)
	session := newSessionID("carry")

	first := askDeployedSession(t, name, env.Location,
		"Remember this number: 8675309. Just acknowledge it.", session)
	t.Logf("turn 1: %s", first)

	second := askDeployedSession(t, name, env.Location,
		"Earlier in this conversation I gave you a number. Repeat it back. "+
			"The number is in the messages above.", session)
	t.Logf("turn 2: %s", second)

	if !strings.Contains(second, "8675309") {
		t.Errorf("second turn %q did not recall the first; the session did not carry", second)
	}
}

// TestDeployed_SessionsAreIsolated checks one conversation cannot read another.
func TestDeployed_SessionsAreIsolated(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	askDeployedSession(t, name, env.Location,
		"Remember this number: 8675309. Just acknowledge it.", newSessionID("iso-a"))

	other := askDeployedSession(t, name, env.Location,
		"What number did I ask you to remember? "+
			"If you have no record of it, reply exactly: NO_MEMORY",
		newSessionID("iso-b"))
	t.Logf("other session: %s", other)

	if strings.Contains(other, "8675309") {
		t.Errorf("a session read another session's conversation: %q", other)
	}
}

// newSessionID returns a session id unique to this run.
func newSessionID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
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

// TestDeployed_ToolCalling asks a real model to call the pack's tool.
//
// This is the only way to exercise tool calling honestly: the mock provider
// cannot emit a tool call, so no offline test can prove it. The prompt is
// directive to make the model's choice as reliable as a model's choice gets;
// a failure here is worth investigating rather than retrying blindly.
//
// The assertion is the tool's mock status string, which appears nowhere in the
// prompt or the pack. Only a turn that called the tool AND received its result
// can produce it, so this covers the whole path: arena tool_specs → adapter →
// container env → executor registration → tool call → answer.
func TestDeployed_ToolCalling(t *testing.T) {
	env := requireEnv(t)
	name := deployedEngine(t, env)

	status, body, err := authedRequest(http.MethodPost,
		engineURL(name, env.Location, "query"),
		`{"class_method":"query","input":{"message":`+
			`"Use the lookup_order tool to look up order 42, then tell me what it says."}}`)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	t.Logf("tool-calling response: %s", body)

	var got struct {
		Output string `json:"output"`
	}
	if unmarshalErr := json.Unmarshal([]byte(body), &got); unmarshalErr != nil {
		t.Fatalf("unmarshal %q: %v", body, unmarshalErr)
	}
	if got.Output == "" {
		t.Fatal("empty output — a tool-calling turn should still produce a response")
	}

	// Match on a distinctive fragment rather than the whole string: the model
	// paraphrases, but it cannot invent "purple locker".
	if !strings.Contains(strings.ToLower(got.Output), "purple locker") {
		t.Errorf("answer does not carry the tool's result: %q\n"+
			"want the mock status %q to reach the model", got.Output, mockOrderStatus)
	}
}

// --- Destroy and Status ----------------------------------------------------
//
// Both were implemented recently and had never run against Agent Runtime: the
// suite covered deploy and invoke only. Everything below exercises a code path
// whose first execution would otherwise have been someone's real teardown.

// TestDeployed_StatusReportsHealthy checks the adapter's view agrees with
// Agent Runtime's. Apply succeeding says nothing about whether the container
// came up, which is the reason Status looks the engine up rather than trusting
// state.
func TestDeployed_StatusReportsHealthy(t *testing.T) {
	env := requireEnv(t)
	_, state := deployedEngineWithState(t, env)

	resp, err := vertex.NewProvider().Status(context.Background(), &deploy.StatusRequest{
		DeployConfig: deployConfig(t, env),
		PriorState:   state,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if resp.Status != "deployed" {
		t.Errorf("Status = %q, want deployed (resources: %+v)", resp.Status, resp.Resources)
	}
	if len(resp.Resources) == 0 {
		t.Fatal("Status reported no resources for a live deployment")
	}
	if resp.Resources[0].Status != "healthy" {
		t.Errorf("resource = %+v, want healthy", resp.Resources[0])
	}
	// The endpoint link is the one thing a user needs after a deploy, and
	// Status is where they look for it once the apply output has scrolled away.
	if len(resp.Resources[0].Links) == 0 {
		t.Error("a healthy engine should carry the query endpoint link")
	}
}

// TestDeployed_StatusReportsAMissingEngine deletes the engine behind the
// adapter's back. Reporting that is the whole point of looking it up.
func TestDeployed_StatusReportsAMissingEngine(t *testing.T) {
	env := requireEnv(t)
	name, state := deployedEngineWithState(t, env)

	if err := deleteEngine(name); err != nil {
		t.Fatalf("delete engine out of band: %v", err)
	}

	resp, err := vertex.NewProvider().Status(context.Background(), &deploy.StatusRequest{
		DeployConfig: deployConfig(t, env),
		PriorState:   state,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("Status = %q, want degraded (resources: %+v)", resp.Status, resp.Resources)
	}
	if len(resp.Resources) == 0 || resp.Resources[0].Status != "missing" {
		t.Fatalf("resources = %+v, want the engine reported missing", resp.Resources)
	}
	// Nothing to call, so a link would be a promise the deployment cannot keep.
	if len(resp.Resources[0].Links) != 0 {
		t.Errorf("a missing engine must carry no endpoint link, got %+v", resp.Resources[0].Links)
	}
}

// TestDeployed_DestroyRemovesTheEngine is the one that matters most: until
// Destroy existed, teardown was a curl command, and an engine left running with
// min_instances above zero bills continuously.
func TestDeployed_DestroyRemovesTheEngine(t *testing.T) {
	env := requireEnv(t)
	name, state := deployedEngineWithState(t, env)

	provider := vertex.NewProvider()
	req := &deploy.DestroyRequest{
		DeployConfig: deployConfig(t, env),
		PriorState:   state,
	}

	var events []*deploy.DestroyEvent
	if err := provider.Destroy(context.Background(), req,
		func(e *deploy.DestroyEvent) error { events = append(events, e); return nil }); err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	var deleted bool
	for _, e := range events {
		if e.Type == "resource" && e.Resource != nil && e.Resource.Action == deploy.ActionDelete {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("events = %+v, want a delete event for the engine", events)
	}

	// Ask Agent Runtime directly rather than trusting the events. engineURL is
	// not usable here: it appends ":<method>" and a bare resource GET has no
	// method, so an empty one yields a trailing colon that the API rejects as a
	// malformed name — which reads as "still present" rather than "bad URL".
	getURL := fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s",
		env.Location, name)
	if status, body, err := authedRequest(http.MethodGet, getURL, ""); err != nil {
		t.Fatalf("verify deletion: %v", err)
	} else if status != http.StatusNotFound {
		t.Errorf("engine still present after destroy: HTTP %d %s", status, body)
	}

	// Destroy has to converge, or a teardown retried after a partial failure
	// becomes manual work.
	if err := provider.Destroy(context.Background(), req, nil); err != nil {
		t.Errorf("destroying an already-destroyed deployment must be a no-op, got: %v", err)
	}
}
