// Package vertex implements the PromptKit deploy provider for Google Agent
// Runtime (formerly Vertex AI Agent Engine). The API resource is still
// reasoningEngines and the API host is still aiplatform.googleapis.com.
package vertex

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// ProviderName is the provider id used in arena config and the binary name.
const ProviderName = "vertex"

// Provider implements deploy.Provider for Google Agent Runtime.
type Provider struct{}

// NewProvider creates a Provider.
func NewProvider() *Provider {
	return &Provider{}
}

// GetProviderInfo returns metadata about the vertex adapter.
func (p *Provider) GetProviderInfo(_ context.Context) (*deploy.ProviderInfo, error) {
	return &deploy.ProviderInfo{
		Name:         ProviderName,
		Version:      Version,
		Capabilities: []string{"validate"},
		ConfigSchema: configSchema,
	}, nil
}

// ValidateConfig is implemented in Task 6. Until then it reports success for
// any syntactically valid JSON so the CLI handshake works end to end.
func (p *Provider) ValidateConfig(
	_ context.Context, _ *deploy.ValidateRequest,
) (*deploy.ValidateResponse, error) {
	return &deploy.ValidateResponse{Valid: true}, nil
}

// Plan is implemented in Phase 1b-ii.
func (p *Provider) Plan(_ context.Context, _ *deploy.PlanRequest) (*deploy.PlanResponse, error) {
	return nil, fmt.Errorf("plan: %w", ErrNotImplemented)
}

// Apply is implemented in Phase 1b-ii.
func (p *Provider) Apply(
	_ context.Context, _ *deploy.PlanRequest, _ deploy.ApplyCallback,
) (string, error) {
	return "", fmt.Errorf("apply: %w", ErrNotImplemented)
}

// Destroy is implemented in Phase 1b-ii.
func (p *Provider) Destroy(
	_ context.Context, _ *deploy.DestroyRequest, _ deploy.DestroyCallback,
) error {
	return fmt.Errorf("destroy: %w", ErrNotImplemented)
}

// Status is implemented in Phase 1b-ii.
func (p *Provider) Status(
	_ context.Context, _ *deploy.StatusRequest,
) (*deploy.StatusResponse, error) {
	return nil, fmt.Errorf("status: %w", ErrNotImplemented)
}

// Import is implemented in Phase 1b-ii.
func (p *Provider) Import(
	_ context.Context, _ *deploy.ImportRequest,
) (*deploy.ImportResponse, error) {
	return nil, fmt.Errorf("import: %w", ErrNotImplemented)
}
