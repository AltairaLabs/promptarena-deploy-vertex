package vertex

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
)

// reconcilePriorState verifies stored state against what the provider actually
// has, and returns the state to plan against plus a description of any drift.
//
// A plan built only from stored state cannot see changes made outside
// promptarena. If an engine was deleted in the console, the stored state still
// records its resource name, the plan reports an update, and apply then fails
// with ErrEngineNotFound. Verifying first turns that into an honest create.
//
// Resolution rules, chosen so verification can only ever improve the plan:
//
//   - not found    -> dropped, and reported as drift
//   - in flight    -> kept; creation was still running when state was written,
//     so there is no settled resource name to look up and apply
//     already reconciles it
//   - lookup error -> kept; a failed lookup is not evidence of absence, and
//     dropping would plan a create that apply would collide with
//
// Drift is reported in a stable order so plan output does not churn.
func reconcilePriorState(
	ctx context.Context, client gcpClient, prior *State,
) (reconciled *State, drift []string) {
	if prior == nil || len(prior.Engines) == 0 {
		return prior, nil
	}

	names := make([]string, 0, len(prior.Engines))
	for name := range prior.Engines {
		names = append(names, name)
	}
	sort.Strings(names)

	kept := make(map[string]EngineState, len(prior.Engines))
	for _, name := range names {
		engine := prior.Engines[name]

		if engine.InFlight || engine.ResourceName == "" {
			kept[name] = engine
			continue
		}

		if _, err := client.GetEngine(ctx, engine.ResourceName); err != nil {
			if errors.Is(err, ErrEngineNotFound) {
				drift = append(drift, fmt.Sprintf(
					"engine %q (%s) no longer exists and will be recreated",
					name, engine.ResourceName))
				continue
			}
			log.Printf("vertex: could not verify engine %q (%v) — assuming it still exists",
				name, err)
		}
		kept[name] = engine
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
) (verified *State, drift []string) {
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
