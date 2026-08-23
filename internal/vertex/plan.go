package vertex

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/promptarena/deploy"
	"github.com/AltairaLabs/promptarena/deploy/adaptersdk"
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
}

// buildPlan diffs desired against prior state and returns the resource changes.
// It performs no I/O: everything it needs has already been gathered.
func buildPlan(in *planInput) *deploy.PlanResponse {
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
		return deploy.ResourceChange{
			Type:   ResTypeAgentRuntime,
			Name:   name,
			Action: deploy.ActionNoChange,
			Detail: "Up to date",
		}
	}

	return deploy.ResourceChange{
		Type:   ResTypeAgentRuntime,
		Name:   name,
		Action: deploy.ActionUpdate,
		Detail: strings.Join(reasons, ", "),
	}
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
