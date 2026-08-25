package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// agentCardUpdateMask limits the patch to the one field, so nothing else on the
// engine is touched by a call the SDK does not know about.
const agentCardUpdateMask = "spec.agentCard"

// agentCardTimeout caps the patch. It is a single small write, and the engine
// is already serving by the time it runs.
const agentCardTimeout = 30 * time.Second

// errorDetailLimit bounds how much of a failed response is quoted back. Enough
// to carry the API's reason, not enough to bury a log in a body.
const errorDetailLimit = 2048

// specField is the engine field this patch writes under.
const specField = "spec"

// patchAgentCard attaches an A2A Agent Card to a deployed engine.
//
// The field exists in the v1beta1 REST API and is absent from the published
// protos, so the generated client cannot express it and this goes over REST.
// Everything else about the engine still goes through the SDK.
//
// Delete this once agent_card appears in aiplatformpb — TestAgentCardIsStillAbsentFromTheSDK
// fails when that happens, which is the prompt to do it.
func patchAgentCard(
	ctx context.Context, httpClient *http.Client, endpoint, engineName string,
	card map[string]any,
) error {
	if len(card) == 0 {
		return nil
	}

	body, err := json.Marshal(map[string]any{
		specField: map[string]any{"agentCard": card},
	})
	if err != nil {
		return fmt.Errorf("encode agent card: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta1/%s?updateMask=%s",
		strings.TrimRight(endpoint, "/"), engineName, agentCardUpdateMask)

	ctx, cancel := context.WithTimeout(ctx, agentCardTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build agent card request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("patch agent card: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, errorDetailLimit))
		return fmt.Errorf("patch agent card: %s: %s",
			resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}

// agentCardEndpoint is the regional API host for a location.
func agentCardEndpoint(location string) string {
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com", location)
}
