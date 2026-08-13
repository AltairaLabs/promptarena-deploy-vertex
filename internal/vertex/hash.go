package vertex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// hashString returns the hex-encoded SHA-256 of s.
func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// hashPack hashes the serialized pack. The CLI hands the pack over as JSON text,
// so the bytes are hashed as received rather than re-serialized — re-encoding
// could reorder keys and produce a spurious diff.
func hashPack(packJSON string) string {
	return hashString(packJSON)
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
}

// hashPlanConfig hashes the deploy-affecting parts of the config plus the
// resolved bindings.
func hashPlanConfig(cfg *Config, resolved []ResolvedBinding) (string, error) {
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
	}

	encoded, err := json.Marshal(fingerprint)
	if err != nil {
		return "", fmt.Errorf("hash deploy config: %w", err)
	}
	return hashString(string(encoded)), nil
}
