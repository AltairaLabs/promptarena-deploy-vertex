package vertex

import (
	"context"
	"fmt"
	"log"

	"github.com/AltairaLabs/promptarena/v2/deploy"
	"github.com/AltairaLabs/promptarena/v2/deploy/adaptersdk"
)

// Every spec update creates an immutable runtime revision, and the engine's
// traffic config decides which revisions serve. These functions are the three
// rollout stages applied to one engine that already exists.

// createEngine creates an engine and records the revision it serves. A new
// engine has nothing else to serve, so whatever the stage, its first revision
// takes all the traffic.
func createEngine(ctx context.Context, client gcpClient, spec *EngineSpec) (*Engine, EngineState, error) {
	engine, err := client.CreateEngine(ctx, spec)
	if err != nil {
		return nil, EngineState{}, err
	}
	entry := EngineState{ResourceName: engine.ResourceName}
	entry.ServingRevision = newestRevisionName(ctx, client, engine.ResourceName)
	return engine, entry, nil
}

// newestRevisionName is best effort: it names the newest revision for state,
// and a failed lookup leaves the field empty rather than failing a deploy that
// has already succeeded.
func newestRevisionName(ctx context.Context, client gcpClient, engine string) string {
	revs, err := client.ListRevisions(ctx, engine)
	if err != nil {
		log.Printf("vertex: could not list the revisions of %q (%v)", engine, err)
		return ""
	}
	if newest := newestRevision(servable(revs)); newest != nil {
		return newest.Name
	}
	return ""
}

// updateLive updates the engine so its new revision serves all the traffic.
//
// Under always-latest routing that is the update alone. Under a manual split
// the update would leave the traffic where it was, so the split is moved to
// the revision the update created; when it created none (the spec did not
// change), to the newest servable revision, which is the one this config
// describes. Plan reports that case, since it can serve a staged candidate.
//
// Once the update has succeeded, a failure still returns the engine's entry:
// any candidate it recorded is no longer what the engine's spec describes,
// and keeping it would let a later promote serve a stale version.
func updateLive(
	ctx context.Context, client gcpClient, existing EngineState, spec *EngineSpec,
) (*Engine, EngineState, error) {
	name := existing.ResourceName
	traffic, err := client.GetTraffic(ctx, name)
	if err != nil {
		return nil, EngineState{}, err
	}

	if traffic.AlwaysLatest {
		engine, updateErr := client.UpdateEngine(ctx, name, spec)
		if updateErr != nil {
			return nil, EngineState{}, updateErr
		}
		return engine, EngineState{
			ResourceName:    engine.ResourceName,
			ServingRevision: newestRevisionName(ctx, client, engine.ResourceName),
		}, nil
	}

	before, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, EngineState{}, err
	}
	engine, err := client.UpdateEngine(ctx, name, spec)
	if err != nil {
		return nil, EngineState{}, err
	}
	partial := EngineState{ResourceName: name}
	after, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, partial, fmt.Errorf("updated, but could not find the new revision to serve: %w", err)
	}
	target := createdSince(before, after)
	if target == nil {
		target = newestRevision(servable(after))
	}
	if target == nil {
		return nil, partial, fmt.Errorf("updated, but the engine lists no revisions to serve")
	}
	if pin := pinnedTo(target.Name); !traffic.equal(pin) {
		if err := client.SetTraffic(ctx, name, pin); err != nil {
			return nil, partial, fmt.Errorf("updated, but could not serve the new revision: %w", err)
		}
	}
	return engine, EngineState{ResourceName: engine.ResourceName, ServingRevision: target.Name}, nil
}

// stageCandidate updates the engine without serving the result.
//
// The order is the point: under always-latest routing the split is pinned to
// the serving revision BEFORE the spec changes, because the update alone would
// move every request to the new revision the moment it was ready. A manual
// split is not touched by a spec update, so it is left as it is until the new
// revision exists, and then set once: re-staging a candidate never takes its
// share away in between.
//
// Once the update has succeeded, a failure returns the entry with the serving
// revision and no candidate: the earlier candidate is no longer what the spec
// describes, and promoting it would serve a stale version.
func stageCandidate(
	ctx context.Context, client gcpClient, existing EngineState, spec *EngineSpec, percent int,
) (*Engine, EngineState, error) {
	name := existing.ResourceName
	traffic, err := client.GetTraffic(ctx, name)
	if err != nil {
		return nil, EngineState{}, err
	}
	before, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, EngineState{}, err
	}
	serving, err := servingRevision(traffic, before, existing.ServingRevision)
	if err != nil {
		return nil, EngineState{}, err
	}
	if traffic.AlwaysLatest {
		pin := pinnedTo(serving)
		if pinErr := client.SetTraffic(ctx, name, pin); pinErr != nil {
			return nil, EngineState{}, fmt.Errorf("pin traffic to the serving revision: %w", pinErr)
		}
		traffic = pin
	}

	engine, err := client.UpdateEngine(ctx, name, spec)
	if err != nil {
		return nil, EngineState{}, err
	}
	entry := EngineState{ResourceName: engine.ResourceName, ServingRevision: serving}

	after, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, entry, fmt.Errorf("staged, but could not find the new revision: %w", err)
	}
	candidate := stagedRevision(before, after, existing, serving)
	if candidate == "" {
		log.Printf("vertex: the spec of %q did not change, so no candidate revision was staged", name)
	}

	want := splitBetween(serving, candidate, percent)
	if !traffic.equal(want) {
		if err := client.SetTraffic(ctx, name, want); err != nil {
			return nil, entry, fmt.Errorf("staged, but could not set the candidate's share to %d%%: %w", percent, err)
		}
	}
	entry.CandidateRevision = candidate
	retireCandidate(ctx, client, existing.CandidateRevision, entry)
	return engine, entry, nil
}

// retireCandidate deletes a staged candidate that a newer one has replaced. It
// has no traffic, but a 0% revision keeps a warm instance until it is deleted
// or garbage-collected. Best effort: a failure is logged, since the deploy
// itself succeeded.
func retireCandidate(ctx context.Context, client gcpClient, old string, entry EngineState) {
	if old == "" || old == entry.CandidateRevision || old == entry.ServingRevision {
		return
	}
	if err := client.DeleteRevision(ctx, old); err != nil {
		log.Printf("vertex: could not delete replaced candidate revision %q (%v)", old, err)
	}
}

// restageShare re-applies a staged candidate whose pack and config are
// unchanged. Every spec update creates a revision, even an identical one, so
// sending the spec again would stage a duplicate. Only the share is set.
func restageShare(
	ctx context.Context, client gcpClient, existing EngineState, percent int,
) (*Engine, EngineState, error) {
	name := existing.ResourceName
	traffic, err := client.GetTraffic(ctx, name)
	if err != nil {
		return nil, EngineState{}, err
	}
	want := splitBetween(existing.ServingRevision, existing.CandidateRevision, percent)
	if !traffic.equal(want) {
		if err := client.SetTraffic(ctx, name, want); err != nil {
			return nil, EngineState{}, fmt.Errorf("set the candidate's share to %d%%: %w", percent, err)
		}
	}
	return &Engine{ResourceName: name, State: EngineStateActive}, existing, nil
}

// stagedRevision is the candidate a candidate deploy leaves: the revision the
// update created, or, when it created none because the spec is the one already
// staged, the earlier candidate. "" when nothing is staged.
func stagedRevision(before, after []Revision, existing EngineState, serving string) string {
	if candidate := createdSince(before, after); candidate != nil {
		return candidate.Name
	}
	if existing.CandidateRevision != serving {
		return existing.CandidateRevision
	}
	return ""
}

// promoteEngines moves each engine's traffic to its staged candidate. It
// changes no spec, creates no revision and deletes nothing, so the pack and
// config hashes stay as the candidate deploy left them.
//
// Every agent must have a candidate before any traffic moves: promoting half a
// multi-agent pack would leave it serving two versions.
func promoteEngines(
	ctx context.Context, client gcpClient, location string, agents []AgentInfo, prior *State,
	report *adaptersdk.ProgressReporter,
) (*State, error) {
	for i := range agents {
		entry, ok := prior.Engines[agents[i].Name]
		if !ok || entry.CandidateRevision == "" {
			return prior, fmt.Errorf(
				"agent %q has no staged candidate to promote: apply with rollout_stage %q first",
				agents[i].Name, RolloutStageCandidate)
		}
	}

	next := prior.clone()
	next.Version = StateVersion
	next.AdapterVersion = Version
	for i := range agents {
		name := agents[i].Name
		entry := next.Engines[name]
		progress(report, fmt.Sprintf("Promoting %s", name),
			float64(i)/float64(len(agents))*applyProgressSpan)

		if err := client.SetTraffic(ctx, entry.ResourceName, pinnedTo(entry.CandidateRevision)); err != nil {
			if i == 0 {
				return prior, fmt.Errorf("agent %q: promote: %w", name, err)
			}
			return next, fmt.Errorf("agent %q: promote: %w", name, err)
		}
		entry.ServingRevision = entry.CandidateRevision
		entry.CandidateRevision = ""
		next.Engines[name] = entry

		resource(report, name, deploy.ActionUpdate, endpointLinks(location, entry.ResourceName)...)
	}
	return next, nil
}
