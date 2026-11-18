package vertex

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/promptarena/v2/deploy"
	"github.com/AltairaLabs/promptarena/v2/deploy/adaptersdk"
)

// Image mode values.
const (
	// ImageModePrebuilt references a published runtime image through the user's
	// Artifact Registry. This is the default.
	ImageModePrebuilt = "prebuilt"
	// ImageModeCloudBuild builds a runtime image inside the user's project.
	ImageModeCloudBuild = "cloudbuild"
)

// DefaultPackInlineLimitBytes is the serialized pack size above which the pack
// is staged to GCS instead of injected as an environment variable. The Cloud Run
// substrate caps total environment size near 32 KiB; this leaves headroom for
// the other injected variables.
const DefaultPackInlineLimitBytes = 24576

// gcsScheme prefixes a Cloud Storage URI.
const gcsScheme = "gs://"

// Observability configures what the deployed engine reports.
//
// Tracing is off by default: an unconfigured deployment should pay nothing and
// send nothing. When on, the runtime emits spans for pipeline, provider and
// tool events, and records eval scores as gen_ai.evaluation.score — scores that
// every turn already computes and otherwise discards.
type Observability struct {
	TracingEnabled bool   `json:"tracing_enabled,omitempty"`
	OTLPEndpoint   string `json:"otlp_endpoint,omitempty"`
}

// ResourceLimits carries the Agent Runtime CPU and memory request.
type ResourceLimits struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// Config holds vertex-specific deploy configuration.
type Config struct {
	Project       string `json:"project"`
	Location      string `json:"location"`
	StagingBucket string `json:"staging_bucket,omitempty"`

	ImageMode         string `json:"image_mode,omitempty"`
	Image             string `json:"image,omitempty"`
	RuntimeBinaryPath string `json:"runtime_binary_path,omitempty"`
	DockerfilePath    string `json:"dockerfile_path,omitempty"`

	ServiceAccount       string          `json:"service_account,omitempty"`
	ResourceLimits       *ResourceLimits `json:"resource_limits,omitempty"`
	MinInstances         *int            `json:"min_instances,omitempty"`
	MaxInstances         *int            `json:"max_instances,omitempty"`
	ContainerConcurrency *int            `json:"container_concurrency,omitempty"`

	// Env sets additional container environment variables on the engine.
	//
	// A live tool's headers_from_env names variables the runtime reads at call
	// time, and without this there is no way to put them there. Values are
	// stored on the engine and readable by anyone who can get it, so a real
	// secret belongs in Secret Manager rather than here.
	Env map[string]string `json:"env,omitempty"`

	Labels               map[string]string `json:"labels,omitempty"`
	PackInlineLimitBytes int               `json:"pack_inline_limit_bytes,omitempty"`
	DryRun               bool              `json:"dry_run,omitempty"`

	Providers     []ProviderBinding `json:"providers,omitempty"`
	Observability *Observability    `json:"observability,omitempty"`

	// RuntimeOverride runs another container in place of the runtime image,
	// with extra environment and Secret Manager variables. The engine still
	// receives the pack and agent environment it always does. Agent Runtime
	// takes an image and no command, so command and args are refused.
	RuntimeOverride *deploy.RuntimeOverride `json:"runtime_override,omitempty"`
}

// runtimeOverrideSupport is what Agent Runtime can apply of a runtime
// override. Its container spec carries an image URI and nothing else, so the
// image's own entrypoint always runs.
var runtimeOverrideSupport = adaptersdk.RuntimeOverrideSupport{
	SecretEnv:           true,
	ReservedEnvPrefixes: []string{envPrefix},
	// Agent Runtime injects these itself and rejects a create that sets them.
	// The adapter sets the OTLP endpoint when tracing is on.
	ReservedEnvNames: []string{"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", envOTLPEndpoint},
}

// image is the image the engine runs: the override's, when it names one.
func (c *Config) image() string {
	if c.RuntimeOverride != nil && c.RuntimeOverride.Image != "" {
		return c.RuntimeOverride.Image
	}
	return c.Image
}

// parseConfig unmarshals the provider config JSON and applies defaults.
func parseConfig(raw string) (*Config, error) {
	var cfg Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}
	cfg.applyDefaults()
	cfg.normalizeRuntimeOverride()
	return &cfg, nil
}

// applyDefaults fills in values the user may omit.
func (c *Config) applyDefaults() {
	if c.ImageMode == "" {
		c.ImageMode = ImageModePrebuilt
	}
	if c.PackInlineLimitBytes == 0 {
		c.PackInlineLimitBytes = DefaultPackInlineLimitBytes
	}
}

// normalizeRuntimeOverride gives configs that deploy the same thing the same
// form, so they hash the same: an empty block is no block, and a secret with
// no version names the latest one explicitly.
func (c *Config) normalizeRuntimeOverride() {
	if c.RuntimeOverride.IsZero() {
		c.RuntimeOverride = nil
		return
	}
	for name, ref := range c.RuntimeOverride.SecretEnv {
		if ref.Version == "" {
			ref.Version = latestSecretVersion
			c.RuntimeOverride.SecretEnv[name] = ref
		}
	}
}

// validateStructure checks the config's own fields. Provider bindings and
// labels are validated separately so their errors read independently.
func (c *Config) validateStructure() []string {
	var errs []string

	if c.Project == "" {
		errs = append(errs, "project is required")
	}
	if c.Location == "" {
		errs = append(errs, "location is required (e.g. us-central1)")
	}

	errs = append(errs, c.validateImageMode()...)

	if c.StagingBucket != "" && !strings.HasPrefix(c.StagingBucket, gcsScheme) {
		errs = append(errs, fmt.Sprintf(
			"staging_bucket %q must start with %s", c.StagingBucket, gcsScheme))
	}

	errs = append(errs, c.validateObservability()...)
	errs = append(errs, c.validateScaling()...)
	errs = append(errs, c.validateRuntimeOverride()...)

	return errs
}

// validateObservability checks that tracing has somewhere to send spans.
// Enabling it without an endpoint would deploy silently untraced, which is
// worse than refusing the config.
func (c *Config) validateObservability() []string {
	if c.Observability == nil {
		return nil
	}
	if c.Observability.TracingEnabled && c.Observability.OTLPEndpoint == "" {
		return []string{
			"observability.otlp_endpoint is required when observability.tracing_enabled is true",
		}
	}

	// The exporter builds its target with otlptracehttp.WithEndpointURL, which
	// needs a full URL. A host:port value yields "http:///v1/traces" — no host —
	// and every export fails at runtime while the deployment looks healthy.
	if c.Observability.OTLPEndpoint != "" &&
		!strings.HasPrefix(c.Observability.OTLPEndpoint, "http://") &&
		!strings.HasPrefix(c.Observability.OTLPEndpoint, "https://") {
		return []string{fmt.Sprintf(
			"observability.otlp_endpoint %q must be a full URL including scheme "+
				"(for example http://collector:4318), not host:port",
			c.Observability.OTLPEndpoint)}
	}

	return nil
}

// validateImageMode checks image_mode and the fields it requires.
func (c *Config) validateImageMode() []string {
	switch c.ImageMode {
	case ImageModePrebuilt:
		if c.image() == "" {
			return []string{
				"image is required in prebuilt mode: an Artifact Registry reference to " +
					"the runtime image (an AR remote repository can proxy ghcr.io), " +
					"or runtime_override.image",
			}
		}
	case ImageModeCloudBuild:
		// Refused rather than accepted and ignored. Apply never built
		// anything for this mode, and since cloudbuild leaves image empty the
		// engine was created with an empty image URI — a deploy that reported
		// success and produced something that could not run.
		return []string{
			"image_mode \"cloudbuild\" is not implemented: nothing builds the " +
				"image, so the engine would be created with no image at all. " +
				"Build the runtime image yourself and use image_mode \"prebuilt\" " +
				"with an Artifact Registry reference to it",
		}
	default:
		return []string{fmt.Sprintf(
			"image_mode %q must be %q or %q", c.ImageMode, ImageModePrebuilt, ImageModeCloudBuild)}
	}
	return nil
}

// validateScaling checks the instance and concurrency bounds Agent Runtime accepts.
func (c *Config) validateScaling() []string {
	var errs []string

	if c.MinInstances != nil && *c.MinInstances < 0 {
		errs = append(errs, "min_instances must not be negative")
	}
	if c.MaxInstances != nil && *c.MaxInstances < 1 {
		errs = append(errs, "max_instances must be at least 1")
	}
	if c.MinInstances != nil && c.MaxInstances != nil && *c.MinInstances > *c.MaxInstances {
		errs = append(errs, fmt.Sprintf(
			"min_instances %d must not exceed max_instances %d",
			*c.MinInstances, *c.MaxInstances))
	}
	if c.ContainerConcurrency != nil && *c.ContainerConcurrency < 1 {
		errs = append(errs, "container_concurrency must be at least 1")
	}

	return errs
}

// validateRuntimeOverride checks the override against what Agent Runtime can
// apply, and refuses a variable set both in env and in the override: the two
// are merged into one environment, and which value won would be an accident.
func (c *Config) validateRuntimeOverride() []string {
	o := c.RuntimeOverride
	if o == nil {
		return nil
	}
	errs := adaptersdk.ValidateRuntimeOverride(o, runtimeOverrideSupport)
	for _, name := range sortedKeys(o.Env) {
		if _, dup := c.Env[name]; dup {
			errs = append(errs, fmt.Sprintf(
				"runtime_override.env %q is also set in env: set it in one place", name))
		}
	}
	for _, name := range sortedKeys(secretNames(o.SecretEnv)) {
		if _, dup := c.Env[name]; dup {
			errs = append(errs, fmt.Sprintf(
				"runtime_override.secret_env %q is also set in env: set it in one place", name))
		}
	}
	return errs
}

// secretNames returns the variable names of a secret env map, as a set.
func secretNames(m map[string]deploy.SecretRef) map[string]string {
	out := make(map[string]string, len(m))
	for name := range m {
		out[name] = ""
	}
	return out
}
