package vertex

import (
	"context"
	"errors"
	"log"
	"sort"

	"github.com/AltairaLabs/promptarena/deploy"
	"github.com/AltairaLabs/promptarena/deploy/adaptersdk"
)

// engineProbe answers the shared drift contract's existence question against
// the Agent Runtime control plane.
//
// Only ErrEngineNotFound is evidence of absence. Everything else is returned as
// an error, which ReconcilePriorState treats as "keep" — the conservative
// outcome. This adapter is the reason that rule is spelled out in the SDK: a
// malformed resource name comes back InvalidArgument rather than NotFound, so
// any "the lookup failed, so it must be gone" shortcut would drop live engines
// and plan a create that collides at apply time.
type engineProbe struct {
	client gcpClient
}

// Exists reports whether the engine behind ref is still present.
func (p engineProbe) Exists(
	ctx context.Context, ref adaptersdk.ResourceRef,
) (adaptersdk.Existence, error) {
	if _, err := p.client.GetEngine(ctx, ref.ID); err != nil {
		if errors.Is(err, ErrEngineNotFound) {
			return adaptersdk.ExistsNo, nil
		}
		// Logged here rather than swallowed: the shared reconciler keeps the
		// resource on error, and an operator reading a plan should still learn
		// that it was not actually verified.
		log.Printf("vertex: could not verify engine %q (%v) — assuming it still exists",
			ref.Name, err)
		return adaptersdk.ExistsUnknown, err
	}
	return adaptersdk.ExistsYes, nil
}

// reconcilePriorState verifies stored state against what the provider actually
// has, and returns the state to plan against plus any drift.
//
// A plan built only from stored state cannot see changes made outside
// promptarena. If an engine was deleted in the console, the stored state still
// records its resource name, the plan reports an update, and apply then fails
// with ErrEngineNotFound. Verifying first turns that into an honest create,
// with a DRIFT change explaining why.
//
// In-flight engines are kept without probing: creation was still running when
// state was written, so there is no settled resource name to look up, and apply
// already reconciles them.
//
// Engines are probed in a stable order so plan output does not churn.
func reconcilePriorState(
	ctx context.Context, client gcpClient, prior *State,
) (reconciled *State, drift []deploy.ResourceChange) {
	if prior == nil || len(prior.Engines) == 0 {
		return prior, nil
	}

	names := make([]string, 0, len(prior.Engines))
	for name := range prior.Engines {
		names = append(names, name)
	}
	sort.Strings(names)

	kept := make(map[string]EngineState, len(prior.Engines))
	refs := make([]adaptersdk.ResourceRef, 0, len(prior.Engines))
	for _, name := range names {
		engine := prior.Engines[name]
		if engine.InFlight || engine.ResourceName == "" {
			kept[name] = engine
			continue
		}
		refs = append(refs, adaptersdk.ResourceRef{
			Type: ResTypeAgentRuntime,
			Name: name,
			ID:   engine.ResourceName,
		})
	}

	survivors, drift := adaptersdk.ReconcilePriorState(ctx, engineProbe{client: client}, refs)
	for _, ref := range survivors {
		kept[ref.Name] = prior.Engines[ref.Name]
	}

	out := *prior
	out.Engines = kept
	return &out, drift
}

// verifiedPriorState returns the prior state Plan should diff against.
//
// Dry run is the offline mode and makes no control-plane calls: its client
// knows about no engines at all, so verifying against it would report every
// engine as drifted. Any failure to reach the provider falls back to stored
// state, so planning never becomes dependent on connectivity it did not
// previously need.
func (p *Provider) verifiedPriorState(
	ctx context.Context, cfg *Config, prior *State,
) (verified *State, drift []deploy.ResourceChange) {
	if cfg.DryRun || prior == nil || len(prior.Engines) == 0 {
		return prior, nil
	}

	client, err := p.newClient(ctx, cfg)
	if err != nil {
		log.Printf("vertex: could not verify deployed state (%v) — "+
			"planning against stored state, which may be out of date", err)
		return prior, nil
	}
	return reconcilePriorState(ctx, client, prior)
}
