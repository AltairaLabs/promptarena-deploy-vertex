package vertex

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
)

// TestPatchAgentCard_SendsTheCardAndNothingElse pins the request shape.
//
// The update mask matters: this call goes outside the SDK, and a patch without
// one would replace parts of an engine the generated client is responsible for.
func TestPatchAgentCard_SendsTheCardAndNothingElse(t *testing.T) {
	var gotMethod, gotPath, gotMask string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotMask = r.URL.Query().Get("updateMask")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	name := "projects/p/locations/us-central1/reasoningEngines/123"
	card := map[string]any{"name": "assistant", "protocolVersion": "0.3.0"}

	if err := patchAgentCard(context.Background(), srv.Client(), srv.URL, name, card); err != nil {
		t.Fatalf("patchAgentCard: %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if want := "/v1beta1/" + name; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotMask != agentCardUpdateMask {
		t.Errorf("updateMask = %q, want %q — without it the patch would touch "+
			"fields the SDK owns", gotMask, agentCardUpdateMask)
	}

	spec, _ := gotBody["spec"].(map[string]any)
	sent, _ := spec["agentCard"].(map[string]any)
	if sent["name"] != "assistant" {
		t.Errorf("card not sent under spec.agentCard: %v", gotBody)
	}
}

// TestPatchAgentCard_NoCardIsNoCall covers a single-prompt pack.
//
// Cards are generated only for packs declaring agents, and agentCard is
// optional — so no card means no request, not an empty one.
func TestPatchAgentCard_NoCardIsNoCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer srv.Close()

	if err := patchAgentCard(context.Background(), srv.Client(), srv.URL, "engine", nil); err != nil {
		t.Fatalf("patchAgentCard: %v", err)
	}
	if called {
		t.Error("a pack with no agent card should not produce a request")
	}
}

// TestPatchAgentCard_ReportsTheAPIsReason keeps a rejection legible.
func TestPatchAgentCard_ReportsTheAPIsReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"agentCard.protocolVersion is required"}}`))
	}))
	defer srv.Close()

	err := patchAgentCard(context.Background(), srv.Client(), srv.URL, "engine",
		map[string]any{"name": "assistant"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "protocolVersion") {
		t.Errorf("error should carry what the API said, got %v", err)
	}
}

// TestAgentCardEndpoint checks the regional host.
func TestAgentCardEndpoint(t *testing.T) {
	if got := agentCardEndpoint("europe-west4"); got != "https://europe-west4-aiplatform.googleapis.com" {
		t.Errorf("agentCardEndpoint = %q", got)
	}
}

// TestAgentCardIsStillAbsentFromTheSDK is the removal criterion for this whole
// file.
//
// agent_card is live in the v1beta1 REST API and missing from the published
// protos, which is the only reason the patch above exists. When Google
// publishes it the generated type gains the field, this test fails, and the
// bespoke path should be deleted in favour of the SDK.
func TestAgentCardIsStillAbsentFromTheSDK(t *testing.T) {
	spec := &aiplatformpb.ReasoningEngineSpec{}
	fields := spec.ProtoReflect().Descriptor().Fields()

	for i := 0; i < fields.Len(); i++ {
		if name := string(fields.Get(i).Name()); name == "agent_card" {
			t.Fatal("ReasoningEngineSpec now has agent_card: delete agentcard_patch.go " +
				"and set the field through the SDK instead")
		}
	}
}

// TestDeploymentSpec_CarriesContainerConcurrency covers config that used to be
// accepted and dropped.
//
// The field was left unmapped because the published protos had none; the
// generated client carries it now, so a value in the config reaches the engine.
func TestDeploymentSpec_CarriesContainerConcurrency(t *testing.T) {
	concurrency := 4
	out := deploymentSpecFrom(&EngineSpec{ContainerConcurrency: &concurrency})
	if out == nil {
		t.Fatal("container_concurrency alone should still produce a deployment spec")
	}
	if out.GetContainerConcurrency() != 4 {
		t.Errorf("ContainerConcurrency = %d, want 4", out.GetContainerConcurrency())
	}
}
