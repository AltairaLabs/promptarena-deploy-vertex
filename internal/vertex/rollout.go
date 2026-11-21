package vertex

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rollout stages: what an apply does with the engine's traffic.
const (
	// RolloutStageLive serves the new revision at once. It is the default.
	RolloutStageLive = "live"
	// RolloutStageCandidate stages the new revision without serving it: the
	// revision that was serving keeps the traffic.
	RolloutStageCandidate = "candidate"
	// RolloutStagePromote moves the traffic to the staged candidate and changes
	// nothing else.
	RolloutStagePromote = "promote"
)

// fullPercent is the whole of an engine's traffic.
const fullPercent = 100

// TrafficTarget is one revision's share of an engine's traffic.
type TrafficTarget struct {
	// Revision is the full projects/…/runtimeRevisions/… name.
	Revision string
	Percent  int
}

// Traffic is an engine's traffic configuration: either every request goes to
// the latest revision (Agent Runtime's default), or it is split manually.
type Traffic struct {
	AlwaysLatest bool
	Targets      []TrafficTarget
}

// Revision is one immutable runtime revision of an engine. Agent Runtime
// creates one each time the engine's spec changes.
type Revision struct {
	// Name is the full projects/…/reasoningEngines/…/runtimeRevisions/… name.
	Name       string
	State      string
	CreateTime time.Time
	// PackVersion is the pack version the revision runs, read from the
	// PROMPTPACK_VERSION variable in its spec. Empty for a revision deployed
	// before the adapter set it.
	PackVersion string
}

// pinnedTo is a manual split sending everything to one revision.
func pinnedTo(revision string) *Traffic {
	return &Traffic{Targets: []TrafficTarget{{Revision: revision, Percent: fullPercent}}}
}

// splitBetween is a manual split giving candidate percent of the traffic and
// stable the rest. A zero percent pins stable, so the candidate is not listed
// as a target at all.
func splitBetween(stable, candidate string, percent int) *Traffic {
	if percent <= 0 {
		return pinnedTo(stable)
	}
	return &Traffic{Targets: []TrafficTarget{
		{Revision: stable, Percent: fullPercent - percent},
		{Revision: candidate, Percent: percent},
	}}
}

// equal reports whether two traffic configurations route the same way.
func (t *Traffic) equal(o *Traffic) bool {
	if t == nil || o == nil {
		return t == o
	}
	if t.AlwaysLatest || o.AlwaysLatest {
		return t.AlwaysLatest == o.AlwaysLatest
	}
	a, b := t.percents(), o.percents()
	if len(a) != len(b) {
		return false
	}
	for rev, pct := range a {
		if b[rev] != pct {
			return false
		}
	}
	return true
}

// percents maps each target revision to its share, leaving out 0% targets,
// which route nothing.
func (t *Traffic) percents() map[string]int {
	out := map[string]int{}
	for _, target := range t.Targets {
		if target.Percent > 0 {
			out[target.Revision] += target.Percent
		}
	}
	return out
}

// revisionID is the trailing id of a revision name, or the name itself.
func revisionID(name string) string {
	if i := strings.LastIndex(name, "/"); i >= 0 {
		return name[i+1:]
	}
	return name
}

// newestRevision returns the most recently created revision, or nil. Ties on
// create time fall back to the numeric id, which Agent Runtime assigns in
// order.
func newestRevision(revs []Revision) *Revision {
	if len(revs) == 0 {
		return nil
	}
	sorted := append([]Revision(nil), revs...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if !sorted[i].CreateTime.Equal(sorted[j].CreateTime) {
			return sorted[i].CreateTime.Before(sorted[j].CreateTime)
		}
		return numericID(sorted[i].Name) < numericID(sorted[j].Name)
	})
	return &sorted[len(sorted)-1]
}

// numericID parses a revision's trailing id, or -1.
func numericID(name string) int {
	n, err := strconv.Atoi(revisionID(name))
	if err != nil {
		return -1
	}
	return n
}

// createdSince returns the newest revision in after that is not in before:
// the revision a spec update just created, or nil when it created none.
func createdSince(before, after []Revision) *Revision {
	seen := make(map[string]bool, len(before))
	for _, r := range before {
		seen[r.Name] = true
	}
	var fresh []Revision
	for _, r := range after {
		if !seen[r.Name] {
			fresh = append(fresh, r)
		}
	}
	return newestRevision(fresh)
}

// servingRevision is the revision that is currently taking the engine's
// traffic, which a candidate deploy keeps serving.
//
// Under the default always-latest routing it is the newest revision. Under a
// manual split it is the revision with the largest share; when an earlier
// candidate deploy recorded which one was stable and it is still in the split,
// that one wins, so a canary that has been given most of the traffic is not
// mistaken for the stable revision.
func servingRevision(traffic *Traffic, revs []Revision, recorded string) (string, error) {
	if traffic == nil || traffic.AlwaysLatest || len(traffic.percents()) == 0 {
		newest := newestRevision(revs)
		if newest == nil {
			return "", fmt.Errorf("the engine has no runtime revisions to keep serving")
		}
		return newest.Name, nil
	}
	shares := traffic.percents()
	if recorded != "" && shares[recorded] > 0 {
		return recorded, nil
	}
	best, bestPct := "", -1
	for _, target := range traffic.Targets {
		if pct := shares[target.Revision]; pct > bestPct {
			best, bestPct = target.Revision, pct
		}
	}
	return best, nil
}

// describeTraffic renders a split for status and errors, naming each
// revision's pack version when it is known: "2 (v1.1.0) 90%, 3 (v1.2.0) 10%".
func describeTraffic(traffic *Traffic, revs []Revision) string {
	if traffic == nil {
		return "unknown"
	}
	versions := make(map[string]string, len(revs))
	for _, r := range revs {
		versions[r.Name] = r.PackVersion
	}
	if traffic.AlwaysLatest {
		newest := newestRevision(revs)
		if newest == nil {
			return "always latest"
		}
		return "always latest: " + revisionLabel(newest.Name, versions) + " 100%"
	}
	parts := make([]string, 0, len(traffic.Targets))
	for _, target := range traffic.Targets {
		parts = append(parts, fmt.Sprintf("%s %d%%", revisionLabel(target.Revision, versions), target.Percent))
	}
	return strings.Join(parts, ", ")
}

// revisionLabel is a revision's id, with its pack version when known.
func revisionLabel(name string, versions map[string]string) string {
	if v := versions[name]; v != "" {
		return fmt.Sprintf("%s (%s)", revisionID(name), v)
	}
	return revisionID(name)
}
