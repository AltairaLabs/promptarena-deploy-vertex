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

// ValidateConfig parses and validates the provider configuration. Structural
// problems become Errors; advisories become Warnings, which do not make the
// config invalid.
func (p *Provider) ValidateConfig(
	_ context.Context, req *deploy.ValidateRequest,
) (*deploy.ValidateResponse, error) {
	cfg, err := parseConfig(req.Config)
	if err != nil {
		return &deploy.ValidateResponse{
			Valid:  false,
			Errors: []string{err.Error()},
		}, nil
	}

	var errs []string
	errs = append(errs, cfg.validateStructure()...)
	errs = append(errs, validateBindings(cfg.Providers)...)
	errs = append(errs, validateLabels(cfg.Labels)...)

	warnings := bindingWarnings(cfg.Providers)
	warnings = append(warnings, diagnoseConfig(cfg)...)

	return &deploy.ValidateResponse{
		Valid:    len(errs) == 0,
		Errors:   errs,
		Warnings: warnings,
	}, nil
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
