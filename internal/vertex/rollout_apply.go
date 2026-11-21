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
	if newest := newestRevision(revs); newest != nil {
		return newest.Name
	}
	return ""
}

// updateLive updates the engine so its new revision serves all the traffic.
//
// Under always-latest routing that is the update alone. Under a manual split
// the update would leave the traffic where it was, so the split is moved to
// the revision the update created; when it created none (the spec did not
// change), to the newest revision, which is the one this config describes.
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
	after, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, EngineState{}, fmt.Errorf("updated, but could not find the new revision to serve: %w", err)
	}
	target := createdSince(before, after)
	if target == nil {
		target = newestRevision(after)
	}
	if target == nil {
		return nil, EngineState{}, fmt.Errorf("updated, but the engine lists no revisions to serve")
	}
	if pin := pinnedTo(target.Name); !traffic.equal(pin) {
		if err := client.SetTraffic(ctx, name, pin); err != nil {
			return nil, EngineState{}, fmt.Errorf("updated, but could not serve the new revision: %w", err)
		}
	}
	return engine, EngineState{ResourceName: engine.ResourceName, ServingRevision: target.Name}, nil
}

// stageCandidate updates the engine without serving the result.
//
// The order is the point: the split is pinned to the serving revision BEFORE
// the spec changes. Under always-latest routing the update alone would move
// every request to the new revision the moment it was ready.
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
	if pin := pinnedTo(serving); !traffic.equal(pin) {
		if pinErr := client.SetTraffic(ctx, name, pin); pinErr != nil {
			return nil, EngineState{}, fmt.Errorf("pin traffic to the serving revision: %w", pinErr)
		}
	}

	engine, err := client.UpdateEngine(ctx, name, spec)
	if err != nil {
		return nil, EngineState{}, err
	}
	entry := EngineState{ResourceName: engine.ResourceName, ServingRevision: serving}

	after, err := client.ListRevisions(ctx, name)
	if err != nil {
		return nil, EngineState{}, fmt.Errorf("staged, but could not find the new revision: %w", err)
	}
	entry.CandidateRevision = stagedRevision(before, after, existing, serving)
	if entry.CandidateRevision == "" {
		log.Printf("vertex: the spec of %q did not change, so no candidate revision was staged", name)
	}

	if entry.CandidateRevision != "" && percent > 0 {
		split := splitBetween(serving, entry.CandidateRevision, percent)
		if err := client.SetTraffic(ctx, name, split); err != nil {
			return nil, EngineState{}, fmt.Errorf("staged, but could not give the candidate %d%%: %w", percent, err)
		}
	}
	return engine, entry, nil
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
