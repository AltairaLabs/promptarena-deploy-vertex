package vertex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AltairaLabs/promptarena/v2/deploy"
)

// hashString returns the hex-encoded SHA-256 of s.
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hashPack hashes the pack's content, ignoring compilation.created_at.
//
// Every compile stamps a new created_at, so a hash over the raw bytes changed on
// every plan even when the pack had not: every engine planned as an update, and
// a staged pack, whose object is named by this hash, was uploaded again on every
// apply (#87). deploy.PackIdentityChecksum drops that field and re-encodes the
// pack canonically, with sorted keys, so key order cannot produce a diff either.
// It is the same identity the CLI uses for pack_checksum. The "sha256:" prefix
// is dropped so the hash keeps the bare-hex form state has always stored.
func hashPack(packJSON string) string {
	return strings.TrimPrefix(deploy.PackIdentityChecksum([]byte(packJSON)), "sha256:")
}

// planConfigFingerprint is the subset of config that, when changed, means the
// deployed engine must be updated. Fields absent here are deliberately not
// deployed state: DryRun changes adapter behavior, not the engine, and
// PackInlineLimitBytes only selects a delivery mechanism.
//
// Map fields are serialized through encoding/json, which sorts map keys, so the
// fingerprint is stable regardless of iteration order.
type planConfigFingerprint struct {
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

	// Added after the fields above, and omitted when unset, so a deployment
	// that sets neither keeps the hash it already has. Env was deployed but
	// never hashed, so a deployment that sets it plans one update after the
	// upgrade, which applies any env change the old hash missed.
	Env             map[string]string       `json:"env,omitempty"`
	RuntimeOverride *deploy.RuntimeOverride `json:"runtime_override,omitempty"`
}

// hashPlanConfig hashes the deploy-affecting parts of the config, the resolved
// bindings, and the tool specs. All three become container environment, so a
// change to any of them must show up as a diff — otherwise editing a tool's
// mock_result would leave the old value running with the plan reporting no
// change.
func hashPlanConfig(cfg *Config, resolved []ResolvedBinding, toolSpecs string) (string, error) {
	fingerprint := planConfigFingerprint{
		Project:              cfg.Project,
		Location:             cfg.Location,
		Image:                cfg.Image,
		ImageMode:            cfg.ImageMode,
		ServiceAccount:       cfg.ServiceAccount,
		ResourceLimits:       cfg.ResourceLimits,
		MinInstances:         cfg.MinInstances,
		MaxInstances:         cfg.MaxInstances,
		ContainerConcurrency: cfg.ContainerConcurrency,
		Labels:               cfg.Labels,
		Bindings:             resolved,
		Observability:        cfg.Observability,
		ToolSpecs:            toolSpecs,
		Env:                  cfg.Env,
		RuntimeOverride:      cfg.RuntimeOverride,
	}

	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return "", fmt.Errorf("hash deploy config: %w", err)
	}
	return hashString(string(encoded)), nil
}
