// Package vertex implements the PromptKit deploy provider for Google Agent
// Runtime (formerly Vertex AI Agent Engine). The API resource is still
// reasoningEngines and the API host is still aiplatform.googleapis.com.
package vertex

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/promptarena/deploy"
	"github.com/AltairaLabs/promptarena/deploy/adaptersdk"
)

// ProviderName is the provider id used in arena config and the binary name.
const ProviderName = "vertex"

// Provider implements deploy.Provider for Google Agent Runtime.
type Provider struct {
	// clientFunc builds the control-plane client. Tests substitute it; when
	// nil the real newClient is used.
	clientFunc func(ctx context.Context, cfg *Config) (gcpClient, error)
}

// NewProvider creates a Provider.
func NewProvider() *Provider {
	return &Provider{}
}

// newClient returns the control-plane client, honoring a test override.
func (p *Provider) newClient(ctx context.Context, cfg *Config) (gcpClient, error) {
	if p.clientFunc != nil {
		return p.clientFunc(ctx, cfg)
	}
	return newClient(ctx, cfg)
}

// GetProviderInfo returns metadata about the vertex adapter.
func (p *Provider) GetProviderInfo(_ context.Context) (*deploy.ProviderInfo, error) {
	return &deploy.ProviderInfo{
		Name:         ProviderName,
		Version:      Version,
		Capabilities: []string{"validate", "plan", "apply"},
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

// Plan reports the resource changes a deploy would make, without mutating
// anything. The diff comes from the pack and config hashes against prior
// adapter state, after that state has been verified against the live control
// plane so drift is visible. Dry run skips verification and stays fully
// offline.
func (p *Provider) Plan(ctx context.Context, req *deploy.PlanRequest) (*deploy.PlanResponse, error) {
	cfg, err := parseConfig(req.DeployConfig)
	if err != nil {
		return nil, err
	}
	if errs := cfg.validateStructure(); len(errs) != 0 {
		return nil, fmt.Errorf("invalid deploy config: %s", strings.Join(errs, "; "))
	}
	prior, stateErr := parseState(req.PriorState)
	if stateErr != nil {
		return nil, stateErr
	}

	arena, err := parseArenaConfig(req.ArenaConfig)
	if err != nil {
		return nil, err
	}
	resolved, resolveErrs := resolveBindings(cfg.Providers, arena)
	if len(resolveErrs) != 0 {
		return nil, fmt.Errorf("provider bindings: %s", strings.Join(resolveErrs, "; "))
	}
	if _, ok := primaryBinding(resolved); !ok {
		return nil, fmt.Errorf("no provider binding with role %q; one is required", RoleLLM)
	}

	toolSpecs, err := encodeToolSpecs(arena)
	if err != nil {
		return nil, err
	}

	configHash, err := hashPlanConfig(cfg, resolved, toolSpecs)
	if err != nil {
		return nil, err
	}
	agents, err := enumerateAgents(req.PackJSON)
	if err != nil {
		return nil, err
	}

	// Verify stored state against the live control plane so the plan reflects
	// reality rather than the last thing written down.
	prior, drift := p.verifiedPriorState(ctx, cfg, prior)

	return buildPlan(&planInput{
		Agents:      agents,
		Prior:       prior,
		PackHash:    hashPack(req.PackJSON),
		ConfigHash:  configHash,
		Delivery:    decidePackDelivery(req.PackJSON, cfg),
		HasA2ATools: hasA2ATools(req.PackJSON),
		Drift:       drift,
	}), nil
}

// Apply creates or updates the Agent Runtime engines for the pack and returns
// the state to persist. State is returned even on partial failure so that
// engines already created are not orphaned.
func (p *Provider) Apply(
	ctx context.Context, req *deploy.PlanRequest, callback deploy.ApplyCallback,
) (string, error) {
	in, agents, prior, err := gatherApplyInput(req)
	if err != nil {
		return "", err
	}

	client, err := newClient(ctx, in.Cfg)
	if err != nil {
		return "", err
	}

	var report *adaptersdk.ProgressReporter
	if callback != nil {
		report = adaptersdk.NewProgressReporter(callback)
	}

	next, applyErr := applyEngines(ctx, client, in, agents, prior, report)

	state, marshalErr := next.Marshal()
	if marshalErr != nil {
		return "", marshalErr
	}
	return state, applyErr
}

// Import is implemented in Phase 1b-ii.
func (p *Provider) Import(
	_ context.Context, _ *deploy.ImportRequest,
) (*deploy.ImportResponse, error) {
	return nil, fmt.Errorf("import: %w", ErrNotImplemented)
}
