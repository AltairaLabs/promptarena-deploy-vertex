package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// featuresPackFile is a pack declaring tools, a validator and a template
// variable — the features a trivial pack never exercises.
const featuresPackFile = "testdata/features.pack.json"

// featuresAgent is the only prompt in featuresPackFile.
const featuresAgent = "support"

// contractServer starts the real contract mux backed by the given pack and the
// mock provider, and returns its base URL. No container and no network beyond
// the loopback httptest listener.
func contractServer(t *testing.T, packFile, agentName string) string {
	t.Helper()

	mux := buildMux(
		newTurnFunc(packFile, agentName, mockOpts(), nil),
		newStreamFunc(packFile, agentName, mockOpts(), nil),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// postContract sends a contract request and returns the status and raw body.
func postContract(t *testing.T, url, body string) (int, string) {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("read body: %v", readErr)
	}
	return resp.StatusCode, string(out)
}

func TestContract_FeaturePackLoadsAndAnswers(t *testing.T) {
	base := contractServer(t, featuresPackFile, featuresAgent)

	status, body := postContract(t, base+routeUnary,
		`{"class_method":"query","input":{"message":"where is order 42?"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	var got contractResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, body)
	}
	if got.Output == "" {
		t.Error("expected a non-empty output from the mock provider")
	}
}

// A pack declaring tools builds a different pipeline than one without. This
// asserts the tool-bearing pipeline still serves the contract; whether a model
// chooses to call the tool is a Layer B question the mock provider cannot
// answer.
func TestContract_ToolDeclarationDoesNotBreakTheTurn(t *testing.T) {
	base := contractServer(t, featuresPackFile, featuresAgent)

	status, body := postContract(t, base+routeUnary,
		`{"class_method":"query","input":{"message":"hello"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

func TestContract_UnknownAgentFails(t *testing.T) {
	base := contractServer(t, featuresPackFile, "no-such-agent")

	status, _ := postContract(t, base+routeUnary,
		`{"class_method":"query","input":{"message":"hi"}}`)
	if status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 for an agent that is not in the pack", status)
	}
}

func TestContract_StreamingOverHTTP(t *testing.T) {
	base := contractServer(t, featuresPackFile, featuresAgent)

	status, body := postContract(t, base+routeStream,
		`{"class_method":"stream_query","input":{"message":"hello"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one ndjson line")
	}
	for i, line := range lines {
		var chunk contractResponse
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("line %d is not JSON: %v (%q)", i, err, line)
		}
	}
}

// Each request opens its own conversation, so a second call must succeed on its
// own rather than depending on the first. This pins the current per-request
// isolation: if session sharing is added later, this test should be replaced
// deliberately, not silently.
func TestContract_MultiTurnIsIndependent(t *testing.T) {
	base := contractServer(t, featuresPackFile, featuresAgent)

	for i, msg := range []string{"first question", "second question"} {
		status, body := postContract(t, base+routeUnary,
			`{"class_method":"query","input":{"message":"`+msg+`"}}`)
		if status != http.StatusOK {
			t.Fatalf("turn %d: status = %d, body = %s", i, status, body)
		}
	}
}

func TestContract_MethodAndPayloadErrors(t *testing.T) {
	base := contractServer(t, featuresPackFile, featuresAgent)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"malformed json", `{not json`, http.StatusBadRequest},
		{"missing message", `{"class_method":"query","input":{}}`, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, _ := postContract(t, base+routeUnary, tt.body)
			if status != tt.want {
				t.Errorf("status = %d, want %d", status, tt.want)
			}
		})
	}

	resp, err := http.Get(base + routeUnary)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
}

// guardrailPackFile declares a max_characters limit tight enough that the mock
// provider's response exceeds it, so the guardrail actually fires. A limit the
// response can never reach would assert only that a validator can be
// configured, not that it works.
const guardrailPackFile = "testdata/guardrail.pack.json"

func TestContract_GuardrailFiresAndRewritesTheResponse(t *testing.T) {
	unguarded := contractServer(t, featuresPackFile, featuresAgent)
	guarded := contractServer(t, guardrailPackFile, featuresAgent)

	body := `{"class_method":"query","input":{"message":"hello"}}`

	baseStatus, baseBody := postContract(t, unguarded+routeUnary, body)
	if baseStatus != http.StatusOK {
		t.Fatalf("unguarded status = %d, body = %s", baseStatus, baseBody)
	}

	gotStatus, gotBody := postContract(t, guarded+routeUnary, body)
	if gotStatus != http.StatusOK {
		t.Fatalf("guarded status = %d, body = %s", gotStatus, gotBody)
	}

	var unguardedOut, guardedOut contractResponse
	if err := json.Unmarshal([]byte(baseBody), &unguardedOut); err != nil {
		t.Fatalf("unmarshal unguarded: %v", err)
	}
	if err := json.Unmarshal([]byte(gotBody), &guardedOut); err != nil {
		t.Fatalf("unmarshal guarded: %v", err)
	}

	if guardedOut.Output == unguardedOut.Output {
		t.Errorf("guardrail did not change the response: both were %q — "+
			"a max_characters limit of 5 must rewrite a longer answer",
			guardedOut.Output)
	}
}

// evalsPackFile declares a pack-level eval. Evals reach telemetry through the
// eval runner's EvalCompleted event; guardrails do not emit that event, so a
// pack needs an `evals` section for scores to appear in traces.
const evalsPackFile = "testdata/evals.pack.json"

func TestContract_EvalPackLoadsAndAnswers(t *testing.T) {
	base := contractServer(t, evalsPackFile, featuresAgent)

	status, body := postContract(t, base+routeUnary,
		`{"class_method":"query","input":{"message":"hello"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}

	var got contractResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v (body %s)", err, body)
	}
	if got.Output == "" {
		t.Error("expected a non-empty output from the mock provider")
	}
}

// Registering executors builds a different pipeline than a pack with no tools.
// This asserts the tool-bearing, executor-registered path still serves the
// contract; whether a model calls the tool is a deployed-engine question.
func TestContract_ToolExecutorRegistrationDoesNotBreakTheTurn(t *testing.T) {
	specs := map[string]toolSpec{
		"lookup_order": {Name: "lookup_order", Mode: "mock", MockResult: "Order 42: shipped"},
	}

	mux := buildMux(
		newTurnFunc(featuresPackFile, featuresAgent, mockOpts(), specs),
		newStreamFunc(featuresPackFile, featuresAgent, mockOpts(), specs),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	status, body := postContract(t, srv.URL+routeUnary,
		`{"class_method":"query","input":{"message":"where is order 42?"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}

// An unsupported mode must not break the turn, but must be reported.
func TestContract_UnsupportedToolModeStillServes(t *testing.T) {
	specs := map[string]toolSpec{
		"run_script": {Name: "run_script", Mode: "exec"},
	}

	mux := buildMux(
		newTurnFunc(featuresPackFile, featuresAgent, mockOpts(), specs),
		newStreamFunc(featuresPackFile, featuresAgent, mockOpts(), specs),
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	status, body := postContract(t, srv.URL+routeUnary,
		`{"class_method":"query","input":{"message":"hello"}}`)
	if status != http.StatusOK {
		t.Fatalf("status = %d, body = %s", status, body)
	}
}
