package vertex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// dryRunIDLen is how many hex characters of the display-name hash form a
// synthetic engine id.
const dryRunIDLen = 12

// dryRunClient serves Config.DryRun: it simulates the control plane in memory
// and never contacts GCP. Synthetic resource names are derived from the display
// name, so a dry run produces the same output every time.
type dryRunClient struct {
	project  string
	location string
	engines  map[string]Engine
}

// newDryRunClient creates a dry-run client scoped to the config's project and
// location.
func newDryRunClient(cfg *Config) *dryRunClient {
	return &dryRunClient{
		project:  cfg.Project,
		location: cfg.Location,
		engines:  map[string]Engine{},
	}
}

// resourceName builds a deterministic synthetic resource name.
func (c *dryRunClient) resourceName(displayName string) string {
	sum := sha256.Sum256([]byte(displayName))
	id := hex.EncodeToString(sum[:])[:dryRunIDLen]
	return fmt.Sprintf("projects/%s/locations/%s/reasoningEngines/%s",
		c.project, c.location, id)
}

// CreateEngine records a simulated engine and returns it as ACTIVE.
func (c *dryRunClient) CreateEngine(_ context.Context, spec *EngineSpec) (*Engine, error) {
	engine := Engine{
		ResourceName: c.resourceName(spec.DisplayName),
		DisplayName:  spec.DisplayName,
		Labels:       spec.Labels,
		State:        EngineStateActive,
	}
	c.engines[engine.ResourceName] = engine
	return &engine, nil
}

// UpdateEngine replaces a simulated engine's display name and labels.
func (c *dryRunClient) UpdateEngine(
	_ context.Context, name string, spec *EngineSpec,
) (*Engine, error) {
	if _, ok := c.engines[name]; !ok {
		return nil, fmt.Errorf("%s: %w", name, ErrEngineNotFound)
	}
	engine := Engine{
		ResourceName: name,
		DisplayName:  spec.DisplayName,
		Labels:       spec.Labels,
		State:        EngineStateActive,
	}
	c.engines[name] = engine
	return &engine, nil
}

// GetEngine returns a simulated engine or a wrapped ErrEngineNotFound.
func (c *dryRunClient) GetEngine(_ context.Context, name string) (*Engine, error) {
	engine, ok := c.engines[name]
	if !ok {
		return nil, fmt.Errorf("%s: %w", name, ErrEngineNotFound)
	}
	return &engine, nil
}

// DeleteEngine removes a simulated engine. Deleting a missing engine succeeds,
// matching the real client's idempotent delete.
func (c *dryRunClient) DeleteEngine(_ context.Context, name string) error {
	delete(c.engines, name)
	return nil
}

// ListEnginesByLabel returns simulated engines matching the selector.
func (c *dryRunClient) ListEnginesByLabel(
	_ context.Context, project, location string, labels map[string]string,
) ([]Engine, error) {
	if project != c.project || location != c.location {
		return nil, nil
	}

	var out []Engine
	for _, engine := range c.engines {
		if labelsMatch(engine.Labels, labels) {
			out = append(out, engine)
		}
	}
	return out, nil
}

// labelsMatch reports whether have is a superset of want.
func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// StageObject records nothing and contacts nothing. Dry run is the offline
// mode, so a staged pack is simulated exactly as an engine is: the URI the
// caller derives is deterministic, and no bytes move.
func (c *dryRunClient) StageObject(_ context.Context, _ string, _ []byte) error {
	return nil
}
