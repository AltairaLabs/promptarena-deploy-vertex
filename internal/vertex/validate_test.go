package vertex

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
)

func validate(t *testing.T, cfg string) *deploy.ValidateResponse {
	t.Helper()
	resp, err := NewProvider().ValidateConfig(
		context.Background(), &deploy.ValidateRequest{Config: cfg})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	return resp
}

func TestValidateConfig_MalformedJSON(t *testing.T) {
	resp := validate(t, `{not json`)
	if resp.Valid {
		t.Error("malformed JSON should not be valid")
	}
	if len(resp.Errors) == 0 {
		t.Error("expected an error message")
	}
}

func TestValidateConfig_MissingRequired(t *testing.T) {
	resp := validate(t, `{}`)
	if resp.Valid {
		t.Error("empty config should not be valid")
	}

	joined := strings.Join(resp.Errors, "; ")
	for _, want := range []string{"project", "location", "providers"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected an error mentioning %q, got %v", want, resp.Errors)
		}
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "us-central1",
		"service_account": "agent-runtime@my-project.iam.gserviceaccount.com",
		"image": "us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime",
		"providers": [{"name":"default","role":"llm","type":"gemini","model":"gemini-2.5-flash"}]
	}`)

	if !resp.Valid {
		t.Errorf("expected valid, got errors %v", resp.Errors)
	}
}

func TestValidateConfig_WarnsWithoutDefaultBinding(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "us-central1",
		"image": "us-central1-docker.pkg.dev/my-project/r/i",
		"providers": [
			{"name":"zeta","role":"llm","type":"gemini","model":"m"},
			{"name":"alpha","role":"llm","type":"claude","model":"m"}
		]
	}`)

	if !resp.Valid {
		t.Fatalf("missing default binding is a warning, not an error: %v", resp.Errors)
	}
	if len(resp.Warnings) == 0 {
		t.Fatal("expected a warning naming the primary binding")
	}
	if !strings.Contains(strings.Join(resp.Warnings, "; "), "alpha") {
		t.Errorf("warning should name alpha, got %v", resp.Warnings)
	}
}

func TestValidateConfig_WarnsOnUnknownLocation(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "mars-central1",
		"image": "us-central1-docker.pkg.dev/my-project/r/i",
		"providers": [{"name":"default","type":"gemini","model":"m"}]
	}`)

	if !resp.Valid {
		t.Errorf("an unrecognized location is a warning, not an error: %v", resp.Errors)
	}
	if !strings.Contains(strings.Join(resp.Warnings, "; "), "mars-central1") {
		t.Errorf("expected a location warning, got %v", resp.Warnings)
	}
}

func TestValidateConfig_WarnsOnMissingServiceAccount(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "us-central1",
		"image": "us-central1-docker.pkg.dev/my-project/r/i",
		"providers": [{"name":"default","type":"gemini","model":"m"}]
	}`)

	if !strings.Contains(strings.Join(resp.Warnings, "; "), "service_account") {
		t.Errorf("expected a service_account warning, got %v", resp.Warnings)
	}
}

// TestValidateConfig_RefusesCloudBuild covers the mode that validated and then
// deployed an engine with no image.
func TestValidateConfig_RefusesCloudBuild(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "us-central1",
		"image_mode": "cloudbuild",
		"runtime_binary_path": "./runtime",
		"staging_bucket": "gs://my-bucket",
		"service_account": "agent-runtime@my-project.iam.gserviceaccount.com",
		"providers": [{"name":"default","type":"gemini","model":"m"}]
	}`)

	if resp.Valid {
		t.Fatal("cloudbuild should not validate while nothing builds the image")
	}
	if !strings.Contains(strings.Join(resp.Errors, "; "), "prebuilt") {
		t.Errorf("error should point at prebuilt, got %v", resp.Errors)
	}
}

func TestValidateConfig_LabelCollisionIsAnError(t *testing.T) {
	resp := validate(t, `{
		"project": "my-project",
		"location": "us-central1",
		"image": "us-central1-docker.pkg.dev/my-project/r/i",
		"labels": {"My.Team": "a", "my-team": "b"},
		"providers": [{"name":"default","type":"gemini","model":"m"}]
	}`)

	if resp.Valid {
		t.Error("colliding labels should be invalid")
	}
}
