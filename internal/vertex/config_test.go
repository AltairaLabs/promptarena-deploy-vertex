package vertex

import (
	"strings"
	"testing"
)

func TestParseConfig_Minimal(t *testing.T) {
	cfg, err := parseConfig(`{"project":"p","location":"us-central1"}`)
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.Project != "p" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if cfg.Location != "us-central1" {
		t.Errorf("Location = %q", cfg.Location)
	}
	if cfg.ImageMode != ImageModePrebuilt {
		t.Errorf("ImageMode = %q, want %q (default)", cfg.ImageMode, ImageModePrebuilt)
	}
	if cfg.PackInlineLimitBytes != DefaultPackInlineLimitBytes {
		t.Errorf("PackInlineLimitBytes = %d, want %d",
			cfg.PackInlineLimitBytes, DefaultPackInlineLimitBytes)
	}
}

func TestParseConfig_InvalidJSON(t *testing.T) {
	if _, err := parseConfig(`{not json`); err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestValidateStructure_RequiresProjectAndLocation(t *testing.T) {
	cfg := &Config{}
	errs := cfg.validateStructure()

	joined := strings.Join(errs, "; ")
	if !strings.Contains(joined, "project") {
		t.Errorf("expected a project error, got %v", errs)
	}
	if !strings.Contains(joined, "location") {
		t.Errorf("expected a location error, got %v", errs)
	}
}

func TestValidateStructure_PrebuiltRequiresImage(t *testing.T) {
	cfg := &Config{Project: "p", Location: "us-central1", ImageMode: ImageModePrebuilt}

	errs := cfg.validateStructure()
	if !strings.Contains(strings.Join(errs, "; "), "image") {
		t.Errorf("prebuilt mode without image should error, got %v", errs)
	}
}

func TestValidateStructure_UnknownImageMode(t *testing.T) {
	cfg := &Config{Project: "p", Location: "us-central1", ImageMode: "magic"}

	if !strings.Contains(strings.Join(cfg.validateStructure(), "; "), "image_mode") {
		t.Error("unknown image_mode should error")
	}
}

func TestValidateStructure_StagingBucketScheme(t *testing.T) {
	cfg := &Config{
		Project:       "p",
		Location:      "us-central1",
		ImageMode:     ImageModePrebuilt,
		Image:         "us-central1-docker.pkg.dev/p/r/i",
		StagingBucket: "my-bucket",
	}

	if !strings.Contains(strings.Join(cfg.validateStructure(), "; "), "gs://") {
		t.Error("staging_bucket without a gs:// scheme should error")
	}
}

func TestValidateStructure_InstanceBounds(t *testing.T) {
	minI, maxI := 5, 2
	cfg := &Config{
		Project:      "p",
		Location:     "us-central1",
		ImageMode:    ImageModePrebuilt,
		Image:        "us-central1-docker.pkg.dev/p/r/i",
		MinInstances: &minI,
		MaxInstances: &maxI,
	}

	if !strings.Contains(strings.Join(cfg.validateStructure(), "; "), "min_instances") {
		t.Error("min_instances greater than max_instances should error")
	}
}

func TestValidateStructure_Valid(t *testing.T) {
	cfg := &Config{
		Project:   "p",
		Location:  "us-central1",
		ImageMode: ImageModePrebuilt,
		Image:     "us-central1-docker.pkg.dev/p/ghcr-remote/altairalabs/promptkit-vertex-runtime",
	}

	if errs := cfg.validateStructure(); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidateStructure_TracingRequiresAnEndpoint(t *testing.T) {
	cfg := &Config{
		Project:       "p",
		Location:      "us-central1",
		ImageMode:     ImageModePrebuilt,
		Image:         "us-central1-docker.pkg.dev/p/r/i",
		Observability: &Observability{TracingEnabled: true},
	}

	if !strings.Contains(strings.Join(cfg.validateStructure(), "; "), "otlp_endpoint") {
		t.Error("enabling tracing without an endpoint should be caught at validation, " +
			"not discovered as a silently untraced deployment")
	}
}

// A host:port endpoint produces "http:///v1/traces" in the exporter — no host —
// so every export fails while the deployment looks healthy. Catch it at
// validation instead.
func TestValidateStructure_OTLPEndpointMustBeAURL(t *testing.T) {
	cfg := &Config{
		Project:   "p",
		Location:  "us-central1",
		ImageMode: ImageModePrebuilt,
		Image:     "us-central1-docker.pkg.dev/p/r/i",
		Observability: &Observability{
			TracingEnabled: true,
			OTLPEndpoint:   "collector:4318",
		},
	}

	if !strings.Contains(strings.Join(cfg.validateStructure(), "; "), "full URL") {
		t.Error("a host:port OTLP endpoint should be rejected")
	}
}

func TestValidateStructure_OTLPEndpointURLAccepted(t *testing.T) {
	cfg := &Config{
		Project:   "p",
		Location:  "us-central1",
		ImageMode: ImageModePrebuilt,
		Image:     "us-central1-docker.pkg.dev/p/r/i",
		Observability: &Observability{
			TracingEnabled: true,
			OTLPEndpoint:   "http://collector:4318",
		},
	}

	if errs := cfg.validateStructure(); len(errs) != 0 {
		t.Errorf("a URL endpoint should validate, got %v", errs)
	}
}

// TestValidateStructure_CloudBuildIsRefused covers a mode that used to validate
// and then produce nothing.
//
// Nothing built an image for cloudbuild, and the mode leaves image empty, so
// apply created an engine with no image URI at all — a deploy that reported
// success and left something that could not run. Refusing says so at plan.
func TestValidateStructure_CloudBuildIsRefused(t *testing.T) {
	cfg := &Config{
		Project:           "p",
		Location:          "us-central1",
		ImageMode:         ImageModeCloudBuild,
		RuntimeBinaryPath: "./runtime",
		StagingBucket:     "gs://bucket",
	}

	errs := cfg.validateStructure()
	if len(errs) == 0 {
		t.Fatal("cloudbuild should be refused until something builds the image")
	}
	// The message has to say what to do instead, or an operator is only told
	// that the thing they configured is gone.
	if !strings.Contains(errs[0], ImageModePrebuilt) {
		t.Errorf("error should point at prebuilt, got %q", errs[0])
	}
}
