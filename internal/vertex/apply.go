package vertex

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/AltairaLabs/promptarena/v2/deploy"
	"github.com/AltairaLabs/promptarena/v2/deploy/adaptersdk"
)

// applyProgressSpan is the fraction of the progress bar the engine phase owns.
const applyProgressSpan = 1.0

// newClient returns the control-plane client for this deployment. Dry run gets
// the in-memory implementation; everything else gets the real one.
func newClient(ctx context.Context, cfg *Config) (gcpClient, error) {
	if cfg.DryRun {
		return newDryRunClient(cfg), nil
	}
	return newRealClient(ctx, cfg)
}

// gatherApplyInput parses and resolves everything an apply needs. It performs
// the same validation as Plan so a bad config fails before any API call.
func gatherApplyInput(req *deploy.PlanRequest) (*engineInput, []AgentInfo, *State, error) {
	cfg, err := parseConfig(req.DeployConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	if errs := cfg.validateStructure(); len(errs) != 0 {
		return nil, nil, nil, fmt.Errorf("invalid deploy config: %v", errs)
	}

	prior, err := parseState(req.PriorState)
	if err != nil {
		return nil, nil, nil, err
	}

	arena, err := parseArenaConfig(req.ArenaConfig)
	if err != nil {
		return nil, nil, nil, err
	}
	resolved, resolveErrs := resolveBindings(cfg.Providers, arena)
	if len(resolveErrs) != 0 {
		return nil, nil, nil, fmt.Errorf("provider bindings: %v", resolveErrs)
	}

	toolSpecs, err := encodeToolSpecs(arena)
	if err != nil {
		return nil, nil, nil, err
	}

	configHash, err := hashPlanConfig(cfg, resolved, toolSpecs)
	if err != nil {
		return nil, nil, nil, err
	}

	agents, err := enumerateAgents(req.PackJSON)
	if err != nil {
		return nil, nil, nil, err
	}

	id, err := packID(req.PackJSON)
	if err != nil {
		return nil, nil, nil, err
	}

	cards, err := buildAgentCards(req.PackJSON)
	if err != nil {
		return nil, nil, nil, err
	}

	return &engineInput{
		Cfg:        cfg,
		PackJSON:   req.PackJSON,
		PackID:     id,
		PackHash:   hashPack(req.PackJSON),
		ConfigHash: configHash,
		Bindings:   resolved,
		Delivery:   decidePackDelivery(req.PackJSON, cfg),
		AgentCards: cards,

		ToolSpecsJSON: toolSpecs,
	}, agents, prior, nil
}

// applyEngines creates or updates one engine per agent, deletes engines whose
// agents have left the pack, and returns the state to persist.
//
// State is returned even when the apply fails partway: engines that were
// created must be recorded, or the next apply orphans them. The adapter
// advertises partial_apply_state, so the CLI saves that state in place of the
// stored one. It is therefore built from the prior state: every engine the
// failed apply did not reach, or failed to update, is still recorded.
//
// A failure that changed nothing returns prior itself, so Apply can hand back
// the stored state untouched.
func applyEngines(
	ctx context.Context,
	client gcpClient,
	in *engineInput,
	agents []AgentInfo,
	prior *State,
	report *adaptersdk.ProgressReporter,
) (*State, error) {
	// Stage before building any engine spec: a non-inline pack has no usable
	// spec until its URI exists, so a failure here should stop the deploy
	// rather than surface once per engine.
	stagedURI, err := stagePack(ctx, client, in, prior)
	if err != nil {
		return prior, err
	}
	in.StagedPackURI = stagedURI

	next := prior.clone()
	next.Version = StateVersion
	next.AdapterVersion = Version
	next.StagedPackURI = in.StagedPackURI

	desired := make(map[string]bool, len(agents))
	for i := range agents {
		desired[agents[i].Name] = true
	}

	for i := range agents {
		agent := agents[i]
		progress(report, fmt.Sprintf("Deploying %s", agent.Name),
			float64(i)/float64(len(agents))*applyProgressSpan)

		engine, err := applyOneEngine(ctx, client, in, agent, prior)
		if err != nil {
			return failedEngineState(next, prior, i, agent.Name, err),
				fmt.Errorf("agent %q: %w", agent.Name, err)
		}
		next.Engines[agent.Name] = EngineState{ResourceName: engine.ResourceName}

		resource(report, agent.Name, actionFor(prior, agent.Name),
			endpointLinks(in.Cfg.Location, engine.ResourceName)...)
	}

	// Every desired engine now runs this pack and config, so the hashes can
	// advance. They describe only the desired engines: an engine left over
	// after a failed delete plans as a delete whatever they say.
	next.PackHash = in.PackHash
	next.ConfigHash = in.ConfigHash

	if err := deleteRemoved(ctx, client, next, desired, report); err != nil {
		return next, err
	}

	return next, nil
}

// failedEngineState is the state to return when applying the engine at index
// failed with err.
//
// A prior engine whose update failed still exists, so its entry stays: dropping
// it would make the next apply create a duplicate and orphan the original. An
// engine whose creation started but did not finish is recorded as in-flight
// under the name the provider assigned it.
//
// The hashes are cleared rather than kept or advanced. Once any engine may have
// changed, the engines run a mix of the prior and the new pack, and no single
// hash describes them: the prior one would hide an updated engine if the
// author reverted, the new one would hide an engine not yet reached. With no
// hash, the next plan updates every engine.
//
// The first engine's create failing outright changed nothing, so prior itself
// comes back.
func failedEngineState(next, prior *State, index int, name string, err error) *State {
	var creating *engineCreatingError
	inFlight := errors.As(err, &creating)
	if inFlight {
		next.Engines[name] = EngineState{ResourceName: creating.ResourceName, InFlight: true}
	}

	_, updated := prior.Engines[name]
	if index == 0 && !updated && !inFlight {
		return prior
	}

	next.PackHash = ""
	next.ConfigHash = ""
	return next
}

// applyOneEngine creates or updates a single agent's engine. An engine recorded
// in prior state but absent from the provider is recreated rather than failing:
// state can outlive the resource.
func applyOneEngine(
	ctx context.Context, client gcpClient, in *engineInput, agent AgentInfo, prior *State,
) (*Engine, error) {
	spec, errs := buildEngine(in, agent)
	if len(errs) != 0 {
		return nil, fmt.Errorf("build engine spec: %v", errs)
	}

	existing, recorded := prior.Engines[agent.Name]
	if !recorded || existing.ResourceName == "" {
		return client.CreateEngine(ctx, spec)
	}

	engine, err := client.UpdateEngine(ctx, existing.ResourceName, spec)
	if errors.Is(err, ErrEngineNotFound) {
		return client.CreateEngine(ctx, spec)
	}
	if err != nil {
		return nil, err
	}
	return engine, nil
}

// deleteRemoved deletes engines whose agents are no longer in the pack.
func deleteRemoved(
	ctx context.Context,
	client gcpClient,
	next *State,
	desired map[string]bool,
	report *adaptersdk.ProgressReporter,
) error {
	removed := make([]string, 0)
	for name := range next.Engines {
		if !desired[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)

	for _, name := range removed {
		progress(report, fmt.Sprintf("Removing %s", name), applyProgressSpan)

		if err := client.DeleteEngine(ctx, next.Engines[name].ResourceName); err != nil {
			return fmt.Errorf("delete engine for agent %q: %w", name, err)
		}
		delete(next.Engines, name)

		resource(report, name, deploy.ActionDelete)
	}

	return nil
}

// actionFor reports whether an agent was created or updated in this apply.
func actionFor(prior *State, name string) deploy.Action {
	if _, ok := prior.Engines[name]; ok {
		return deploy.ActionUpdate
	}
	return deploy.ActionCreate
}

// statusUnchanged is the protocol status for a resource that was not modified.
const statusUnchanged = "unchanged"

// progress emits a progress event when a reporter is present.
//
// A failed callback is deliberately ignored: progress reporting is advisory,
// and losing a status line must not fail a deployment that is otherwise
// succeeding.
func progress(report *adaptersdk.ProgressReporter, message string, pct float64) {
	if report == nil {
		return
	}
	_ = report.Progress(message, pct)
}

// resource emits a resource event when a reporter is present. As with progress,
// a failed callback is advisory and does not fail the apply.
func resource(
	report *adaptersdk.ProgressReporter, name string, action deploy.Action,
	links ...deploy.ResourceLink,
) {
	if report == nil {
		return
	}
	_ = report.Resource(&deploy.ResourceResult{
		Type:   ResTypeAgentRuntime,
		Name:   name,
		Action: action,
		Status: statusFor(action),
		Links:  links,
	})
}

// statusFor maps an action onto the status string the protocol expects.
func statusFor(action deploy.Action) string {
	switch action {
	case deploy.ActionCreate:
		return "created"
	case deploy.ActionUpdate:
		return "updated"
	case deploy.ActionDelete:
		return statusDeleted
	case deploy.ActionNoChange, deploy.ActionDrift:
		return statusUnchanged
	default:
		return statusUnchanged
	}
}
