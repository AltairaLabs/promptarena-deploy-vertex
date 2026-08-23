package vertex

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/AltairaLabs/promptarena/deploy"
)

// Deployment-wide status values the protocol expects.
const (
	StatusDeployed    = "deployed"
	StatusNotDeployed = "not_deployed"
	StatusDegraded    = "degraded"
)

// Per-resource health values the protocol expects.
const (
	ResourceHealthy   = "healthy"
	ResourceUnhealthy = "unhealthy"
	ResourceMissing   = "missing"
)

// Destroy event types the protocol expects.
const (
	eventProgress = "progress"
	eventResource = "resource"
	eventError    = "error"
)

// statusDeleted is the resource status a completed delete reports.
const statusDeleted = "deleted"

// sortedEngineNames returns the agent names in state in a stable order, so
// destroy and status output does not churn between runs.
func sortedEngineNames(state *State) []string {
	names := make([]string, 0, len(state.Engines))
	for name := range state.Engines {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// destroyEvent sends one event, tolerating a nil callback.
func destroyEvent(callback deploy.DestroyCallback, event *deploy.DestroyEvent) error {
	if callback == nil {
		return nil
	}
	return callback(event)
}

// Destroy deletes every engine recorded in state.
//
// Deleting an engine that is already gone is success, not an error: destroy has
// to converge on an already-clean project, or a retried teardown after a
// partial failure becomes manual work. The client's DeleteEngine is already
// idempotent, so this only has to not undo that.
//
// A failed delete does not abort the rest. Stopping at the first error would
// strand the remaining engines — each one billing while it runs — and leave the
// operator to work out which of them survived. Every failure is reported and
// the first is returned once the pass is done.
func (p *Provider) Destroy(
	ctx context.Context, req *deploy.DestroyRequest, callback deploy.DestroyCallback,
) error {
	cfg, err := parseConfig(req.DeployConfig)
	if err != nil {
		return err
	}
	prior, err := parseState(req.PriorState)
	if err != nil {
		return err
	}
	if prior == nil || len(prior.Engines) == 0 {
		return destroyEvent(callback, &deploy.DestroyEvent{
			Type: eventProgress, Message: "Nothing to destroy",
		})
	}

	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return fmt.Errorf("destroy: %w", err)
	}

	var firstErr error
	for _, name := range sortedEngineNames(prior) {
		err := destroyOneEngine(ctx, client, callback, name, prior.Engines[name])
		if errors.Is(err, errCallbackFailed) {
			return errors.Unwrap(err)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// errCallbackFailed marks an error as coming from the caller's callback rather
// than from the provider. A callback that fails means the caller has gone away,
// so there is no point continuing; an engine that fails to delete is just one
// engine, and the rest still need attempting.
var errCallbackFailed = errors.New("destroy callback failed")

// destroyOneEngine deletes a single engine and reports what happened.
func destroyOneEngine(
	ctx context.Context, client gcpClient, callback deploy.DestroyCallback,
	name string, engine EngineState,
) error {
	if err := destroyEvent(callback, &deploy.DestroyEvent{
		Type: eventProgress, Message: fmt.Sprintf("Deleting %s", name),
	}); err != nil {
		return fmt.Errorf("%w: %w", errCallbackFailed, err)
	}

	// An engine recorded mid-creation has no settled resource name, so there is
	// nothing to address. Report it rather than skipping silently: it may still
	// be running, and only the operator can resolve that.
	if engine.ResourceName == "" {
		if err := destroyEvent(callback, &deploy.DestroyEvent{
			Type: eventError,
			Message: fmt.Sprintf(
				"engine %q was still being created when state was written; "+
					"it has no resource name to delete and may need removing by hand", name),
		}); err != nil {
			return fmt.Errorf("%w: %w", errCallbackFailed, err)
		}
		return nil
	}

	if delErr := client.DeleteEngine(ctx, engine.ResourceName); delErr != nil {
		if err := destroyEvent(callback, &deploy.DestroyEvent{
			Type:    eventError,
			Message: fmt.Sprintf("delete engine for agent %q: %v", name, delErr),
		}); err != nil {
			return fmt.Errorf("%w: %w", errCallbackFailed, err)
		}
		return fmt.Errorf("delete engine for agent %q: %w", name, delErr)
	}

	if err := destroyEvent(callback, &deploy.DestroyEvent{
		Type: eventResource,
		Resource: &deploy.ResourceResult{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionDelete,
			Status: statusDeleted,
			Detail: engine.ResourceName,
		},
	}); err != nil {
		return fmt.Errorf("%w: %w", errCallbackFailed, err)
	}
	return nil
}

// Status reports what the provider actually has, engine by engine.
//
// Apply succeeding says nothing about whether a container started, so this
// looks the engines up rather than trusting state. An engine that is gone is
// missing rather than an error: that is drift, and reporting it is the point.
func (p *Provider) Status(
	ctx context.Context, req *deploy.StatusRequest,
) (*deploy.StatusResponse, error) {
	cfg, err := parseConfig(req.DeployConfig)
	if err != nil {
		return nil, err
	}
	prior, err := parseState(req.PriorState)
	if err != nil {
		return nil, err
	}
	if prior == nil || len(prior.Engines) == 0 {
		return &deploy.StatusResponse{
			Status: StatusNotDeployed,
			State:  req.PriorState,
		}, nil
	}

	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}

	resources := make([]deploy.ResourceStatus, 0, len(prior.Engines))
	for _, name := range sortedEngineNames(prior) {
		resources = append(resources,
			engineStatus(ctx, client, cfg, name, prior.Engines[name]))
	}

	return &deploy.StatusResponse{
		Status:    aggregateStatus(resources),
		Resources: resources,
		State:     req.PriorState,
	}, nil
}

// engineStatus looks up one engine and maps what it finds onto the protocol.
func engineStatus(
	ctx context.Context, client gcpClient, cfg *Config,
	name string, engine EngineState,
) deploy.ResourceStatus {
	status := deploy.ResourceStatus{Type: ResTypeAgentRuntime, Name: name}

	// In-flight engines were still being created when state was written, so
	// there is no name to look up. That is not healthy, but it is not missing
	// either — apply reconciles it.
	if engine.InFlight || engine.ResourceName == "" {
		status.Status = ResourceUnhealthy
		status.Detail = "Creation was still in progress when state was written"
		return status
	}

	status.Links = endpointLinks(cfg.Location, engine.ResourceName)

	live, err := client.GetEngine(ctx, engine.ResourceName)
	if err != nil {
		if errors.Is(err, ErrEngineNotFound) {
			status.Status = ResourceMissing
			status.Detail = "Engine no longer exists at the provider"
			// A missing engine has nothing to call, so the link would be a
			// promise the deployment cannot keep.
			status.Links = nil
			return status
		}
		// A failed lookup is not evidence of absence — the same rule the drift
		// contract applies. Report it as unhealthy rather than missing, which
		// would read as "it is gone" when we simply could not tell.
		status.Status = ResourceUnhealthy
		status.Detail = fmt.Sprintf("Could not verify the engine: %v", err)
		return status
	}

	if live.State == EngineStateActive {
		status.Status = ResourceHealthy
		status.Detail = engine.ResourceName
		return status
	}

	status.Status = ResourceUnhealthy
	status.Detail = fmt.Sprintf("Engine state is %s", live.State)
	return status
}

// aggregateStatus rolls the per-engine health up into one deployment status.
//
// Anything short of every engine healthy is degraded rather than deployed. A
// pack whose engines are half up is not a working deployment, and calling it
// "deployed" because most of it is would hide exactly the case worth seeing.
func aggregateStatus(resources []deploy.ResourceStatus) string {
	for i := range resources {
		if resources[i].Status != ResourceHealthy {
			return StatusDegraded
		}
	}
	return StatusDeployed
}
