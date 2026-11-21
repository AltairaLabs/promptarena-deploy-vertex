package vertex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/promptarena/v2/deploy"
	"github.com/AltairaLabs/promptarena/v2/deploy/adaptersdk"
)

// Resource type names surfaced in plans.
const (
	// ResTypeAgentRuntime is one Agent Runtime engine (a reasoningEngine).
	ResTypeAgentRuntime = "agent_runtime"
	// ResTypePackObject is the pack staged to Cloud Storage.
	ResTypePackObject = "pack_object"
)

// stagedPackName is the plan-facing name of the staged pack object.
const stagedPackName = "pack.json"

// planInput is everything buildPlan needs, gathered by Provider.Plan.
type planInput struct {
	Agents      []AgentInfo
	Prior       *State
	PackHash    string
	ConfigHash  string
	Delivery    PackDelivery
	HasA2ATools bool
	// Drift describes engines that were in prior state but no longer exist,
	// as found by verifying against the live control plane. They travel as
	// changes rather than warnings so they are counted and rendered like
	// everything else in the plan.
	Drift []deploy.ResourceChange
	// Stage and CandidatePercent are the rollout options; Stage is never
	// empty (live when unset).
	Stage            string
	CandidatePercent int
}

// buildPlan diffs desired against prior state and returns the resource changes.
// It performs no I/O: everything it needs has already been gathered.
func buildPlan(in *planInput) *deploy.PlanResponse {
	if in.Stage == "" {
		in.Stage = RolloutStageLive
	}
	if in.Stage == RolloutStagePromote {
		changes := planPromote(in)
		return &deploy.PlanResponse{
			Changes:  changes,
			Summary:  summarizeChanges(changes),
			Warnings: promoteWarnings(in),
		}
	}

	// Drift first: each entry explains why the engine below it is being
	// created rather than updated.
	changes := append([]deploy.ResourceChange{}, in.Drift...)
	changes = append(changes, planEngineChanges(in)...)
	if !in.Delivery.Inline {
		changes = append(changes, deploy.ResourceChange{
			Type:   ResTypePackObject,
			Name:   stagedPackName,
			Action: deploy.ActionCreate,
			Detail: fmt.Sprintf("Stage the %d byte pack to Cloud Storage", in.Delivery.SizeBytes),
		})
	}

	return &deploy.PlanResponse{
		Changes:  changes,
		Summary:  summarizeChanges(changes),
		Warnings: planWarnings(in),
	}
}

// planEngineChanges produces one change per desired agent, plus a DELETE for
// each engine in prior state whose agent is no longer in the pack.
func planEngineChanges(in *planInput) []deploy.ResourceChange {
	desired := make(map[string]bool, len(in.Agents))
	changes := make([]deploy.ResourceChange, 0, len(in.Agents))

	for i := range in.Agents {
		name := in.Agents[i].Name
		desired[name] = true
		changes = append(changes, engineChange(in, name))
	}

	removed := make([]string, 0)
	for name := range in.Prior.Engines {
		if !desired[name] {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)

	for _, name := range removed {
		changes = append(changes, deploy.ResourceChange{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionDelete,
			Detail: "Agent is no longer in the pack",
		})
	}

	return changes
}

// engineChange decides the action for one desired agent.
func engineChange(in *planInput, name string) deploy.ResourceChange {
	prior, deployed := in.Prior.Engines[name]

	if !deployed {
		return deploy.ResourceChange{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionCreate,
			Detail: "Create the Agent Runtime engine",
		}
	}

	if prior.InFlight {
		return deploy.ResourceChange{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionUpdate,
			Detail: "Previous creation did not finish; reconcile the engine",
		}
	}

	var reasons []string
	if in.Prior.PackHash != in.PackHash {
		reasons = append(reasons, "pack changed")
	}
	if in.Prior.ConfigHash != in.ConfigHash {
		reasons = append(reasons, "config changed")
	}

	if len(reasons) == 0 {
		return unchangedEngine(in, name, prior)
	}

	return deploy.ResourceChange{
		Type:   ResTypeAgentRuntime,
		Name:   name,
		Action: deploy.ActionUpdate,
		Detail: strings.Join(reasons, ", ") + "; " + trafficIntent(in, prior),
	}
}

// unchangedEngine is the change for an engine whose pack and config are as
// deployed. With a candidate staged it is not a no-op: a live apply serves the
// candidate, since it is the newest revision and the one this config
// describes, and a candidate apply sets its share again.
func unchangedEngine(in *planInput, name string, prior EngineState) deploy.ResourceChange {
	change := deploy.ResourceChange{
		Type:   ResTypeAgentRuntime,
		Name:   name,
		Action: deploy.ActionNoChange,
		Detail: "Up to date",
	}
	if prior.CandidateRevision == "" {
		return change
	}
	candidate := revisionID(prior.CandidateRevision)
	change.Action = deploy.ActionUpdate
	if in.Stage == RolloutStageCandidate {
		change.Detail = fmt.Sprintf("Up to date; candidate revision %s stays staged at %d%%",
			candidate, in.CandidatePercent)
		return change
	}
	change.Detail = fmt.Sprintf("Up to date, but candidate revision %s is staged: "+
		"a live apply serves it at 100%%", candidate)
	return change
}

// trafficIntent says where an update leaves the traffic.
func trafficIntent(in *planInput, prior EngineState) string {
	if in.Stage != RolloutStageCandidate {
		return "the new revision serves 100%"
	}
	serving := revisionPhrase(prior.ServingRevision)
	if in.CandidatePercent > 0 {
		return fmt.Sprintf("stage the new revision as a candidate at %d%%, %s keeps %d%%",
			in.CandidatePercent, serving, fullPercent-in.CandidatePercent)
	}
	return fmt.Sprintf("stage the new revision as a candidate at 0%%, %s keeps 100%%", serving)
}

// revisionPhrase names a recorded revision in a plan, or the serving revision
// generically when none was recorded.
func revisionPhrase(name string) string {
	if name == "" {
		return "the serving revision"
	}
	return "revision " + revisionID(name)
}

// planPromote produces one change per agent: its traffic moving to the staged
// candidate. An agent with nothing staged is reported as such, and apply
// refuses it.
func planPromote(in *planInput) []deploy.ResourceChange {
	changes := make([]deploy.ResourceChange, 0, len(in.Agents))
	for i := range in.Agents {
		name := in.Agents[i].Name
		entry := in.Prior.Engines[name]
		if entry.CandidateRevision == "" {
			changes = append(changes, deploy.ResourceChange{
				Type:   ResTypeAgentRuntime,
				Name:   name,
				Action: deploy.ActionNoChange,
				Detail: "No staged candidate to promote; apply refuses until one is staged",
			})
			continue
		}
		from := revisionPhrase(entry.ServingRevision)
		changes = append(changes, deploy.ResourceChange{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionUpdate,
			Detail: fmt.Sprintf("Promote candidate revision %s to 100%% of traffic (from %s)",
				revisionID(entry.CandidateRevision), from),
		})
	}
	return changes
}

// promoteWarnings flags a promote whose pack or config differs from what was
// staged: promote serves the staged revision, not the current files.
func promoteWarnings(in *planInput) []string {
	if in.Prior.PackHash == in.PackHash && in.Prior.ConfigHash == in.ConfigHash {
		return nil
	}
	return []string{"the pack or config changed since the candidate was staged; promote " +
		"serves the staged candidate as it was deployed, not the current pack"}
}

// planWarnings returns advisories about a plan that will apply cleanly but may
// not behave as the author expects.
func planWarnings(in *planInput) []string {
	var warnings []string

	if in.HasA2ATools {
		warnings = append(warnings,
			"the pack declares a2a__ tools, but agent-to-agent calls are not wired by "+
				"this adapter yet; those tool calls will fail at runtime")
	}

	if len(in.Agents) > 1 {
		warnings = append(warnings, fmt.Sprintf(
			"%d agents deploy as independent engines with no agent-to-agent routing "+
				"between them", len(in.Agents)))
	}

	if !in.Delivery.Inline {
		warnings = append(warnings, fmt.Sprintf(
			"the pack is %d bytes, over the inline limit, so it is delivered through "+
				"Cloud Storage; the engine service account needs read access to the "+
				"staging bucket", in.Delivery.SizeBytes))
	}

	return warnings
}

// summarizeChanges renders a one-line summary of the plan.
//
// This defers to the SDK so DRIFT lands in its own bucket. The local version
// counted it as an update, which was harmless while drift was reported as
// warning text and wrong the moment it became a change.
func summarizeChanges(changes []deploy.ResourceChange) string {
	return adaptersdk.SummarizeChanges(changes)
}
