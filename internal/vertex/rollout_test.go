package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/v2/deploy"
)

// rolloutClient records the order of the calls that matter to a rollout, so a
// test can tell a traffic pin made before the update from one made after.
type rolloutClient struct {
	*dryRunClient
	calls      []string
	setFailure error
}

func (c *rolloutClient) UpdateEngine(ctx context.Context, name string, spec *EngineSpec) (*Engine, error) {
	c.calls = append(c.calls, "update")
	return c.dryRunClient.UpdateEngine(ctx, name, spec)
}

func (c *rolloutClient) SetTraffic(ctx context.Context, name string, t *Traffic) error {
	c.calls = append(c.calls, "traffic:"+describeTraffic(t, nil))
	if c.setFailure != nil {
		return c.setFailure
	}
	return c.dryRunClient.SetTraffic(ctx, name, t)
}

func newRolloutClient() *rolloutClient {
	return &rolloutClient{dryRunClient: newDryRunClient(&Config{Project: "p", Location: "us-central1"})}
}

func rolloutInput(stage string, percent *int, version string) *engineInput {
	in := testApplyInput()
	in.Cfg.RolloutStage = stage
	in.Cfg.CandidatePercent = percent
	in.PackVersion = version
	return in
}

// deployed creates the engine with a first apply and returns its state.
func deployed(t *testing.T, c *rolloutClient) *State {
	t.Helper()
	state, err := applyEngines(context.Background(), c, rolloutInput("", nil, "v1"),
		[]AgentInfo{{Name: "assistant"}}, newState(), nil)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	c.calls = nil
	return state
}

func intPtr(v int) *int { return &v }

func TestCreateRecordsTheServingRevision(t *testing.T) {
	c := newRolloutClient()
	state := deployed(t, c)
	entry := state.Engines["assistant"]
	if revisionID(entry.ServingRevision) != "1" || entry.CandidateRevision != "" {
		t.Fatalf("entry = %+v, want serving 1 and no candidate", entry)
	}
}

// The pin must land before the spec update: under always-latest routing the
// update alone would serve the new revision the moment it was ready.
func TestCandidatePinsTheServingRevisionBeforeTheUpdate(t *testing.T) {
	c := newRolloutClient()
	prior := deployed(t, c)
	engine := prior.Engines["assistant"].ResourceName

	got, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v2"),
		[]AgentInfo{{Name: "assistant"}}, prior, nil)
	if err != nil {
		t.Fatalf("candidate apply: %v", err)
	}

	if want := []string{"traffic:1 100%", "update"}; strings.Join(c.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", c.calls, want)
	}
	traffic, _ := c.GetTraffic(context.Background(), engine)
	if !traffic.equal(pinnedTo(engine + "/runtimeRevisions/1")) {
		t.Errorf("traffic = %s, want revision 1 at 100%%", describeTraffic(traffic, nil))
	}
	entry := got.Engines["assistant"]
	if revisionID(entry.ServingRevision) != "1" || revisionID(entry.CandidateRevision) != "2" {
		t.Errorf("entry = %+v, want serving 1, candidate 2", entry)
	}
	revs, _ := c.ListRevisions(context.Background(), engine)
	if revs[1].PackVersion != "v2" {
		t.Errorf("candidate revision pack version = %q, want v2", revs[1].PackVersion)
	}
}

func TestCandidatePercentSplitsTheTraffic(t *testing.T) {
	c := newRolloutClient()
	prior := deployed(t, c)
	engine := prior.Engines["assistant"].ResourceName

	if _, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, intPtr(10), "v2"),
		[]AgentInfo{{Name: "assistant"}}, prior, nil); err != nil {
		t.Fatalf("candidate apply: %v", err)
	}
	traffic, _ := c.GetTraffic(context.Background(), engine)
	want := splitBetween(engine+"/runtimeRevisions/1", engine+"/runtimeRevisions/2", 10)
	if !traffic.equal(want) {
		t.Errorf("traffic = %s, want 1 90%%, 2 10%%", describeTraffic(traffic, nil))
	}
}

// A second candidate keeps the recorded stable revision serving, not the
// earlier candidate, even when the earlier candidate holds a share.
func TestASecondCandidateKeepsTheStableRevision(t *testing.T) {
	c := newRolloutClient()
	prior := deployed(t, c)
	agents := []AgentInfo{{Name: "assistant"}}

	first, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, intPtr(50), "v2"), agents, prior, nil)
	if err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	second, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v3"), agents, first, nil)
	if err != nil {
		t.Fatalf("second candidate: %v", err)
	}
	entry := second.Engines["assistant"]
	if revisionID(entry.ServingRevision) != "1" || revisionID(entry.CandidateRevision) != "3" {
		t.Errorf("entry = %+v, want serving 1, candidate 3", entry)
	}
}

func TestPromoteServesTheCandidateAndChangesNoSpec(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	staged, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v2"),
		agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	c.calls = nil

	got, err := applyEngines(context.Background(), c, rolloutInput(RolloutStagePromote, nil, "v2"), agents, staged, nil)
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if strings.Join(c.calls, ",") != "traffic:2 100%" {
		t.Errorf("calls = %v, want one traffic change and no update", c.calls)
	}
	entry := got.Engines["assistant"]
	if revisionID(entry.ServingRevision) != "2" || entry.CandidateRevision != "" {
		t.Errorf("entry = %+v, want serving 2 and no candidate", entry)
	}
	if got.PackHash != staged.PackHash || got.ConfigHash != staged.ConfigHash {
		t.Error("promote must leave the hashes as the candidate deploy recorded them")
	}
}

func TestPromoteWithNothingStagedIsRefused(t *testing.T) {
	c := newRolloutClient()
	prior := deployed(t, c)

	got, err := applyEngines(context.Background(), c, rolloutInput(RolloutStagePromote, nil, "v1"),
		[]AgentInfo{{Name: "assistant"}}, prior, nil)
	if err == nil || !strings.Contains(err.Error(), "no staged candidate") {
		t.Fatalf("err = %v, want no staged candidate", err)
	}
	if got != prior || len(c.calls) != 0 {
		t.Errorf("a refused promote must change nothing (calls %v)", c.calls)
	}
}

func TestFailedPromoteOfTheFirstEngineReturnsPrior(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	staged, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v2"),
		agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	c.setFailure = errors.New("boom")

	got, err := applyEngines(context.Background(), c, rolloutInput(RolloutStagePromote, nil, "v2"), agents, staged, nil)
	if err == nil || got != staged {
		t.Errorf("got %v, err %v: want the staged state back and an error", got, err)
	}
}

// Live on an engine left on a manual split moves the split to the new
// revision; without that the update would serve nothing new.
func TestLiveOnAManualSplitServesTheNewRevision(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	staged, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v2"),
		agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	engine := staged.Engines["assistant"].ResourceName

	got, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageLive, nil, "v3"), agents, staged, nil)
	if err != nil {
		t.Fatalf("live: %v", err)
	}
	traffic, _ := c.GetTraffic(context.Background(), engine)
	if !traffic.equal(pinnedTo(engine + "/runtimeRevisions/3")) {
		t.Errorf("traffic = %s, want revision 3 at 100%%", describeTraffic(traffic, nil))
	}
	entry := got.Engines["assistant"]
	if revisionID(entry.ServingRevision) != "3" || entry.CandidateRevision != "" {
		t.Errorf("entry = %+v, want serving 3, no candidate", entry)
	}
}

func TestLiveOnDefaultRoutingLeavesTrafficAlone(t *testing.T) {
	c := newRolloutClient()
	prior := deployed(t, c)
	if _, err := applyEngines(context.Background(), c, rolloutInput("", nil, "v2"),
		[]AgentInfo{{Name: "assistant"}}, prior, nil); err != nil {
		t.Fatalf("live: %v", err)
	}
	if strings.Join(c.calls, ",") != "update" {
		t.Errorf("calls = %v, want the update alone", c.calls)
	}
}

func TestServingRevision(t *testing.T) {
	revs := []Revision{{Name: "e/runtimeRevisions/1"}, {Name: "e/runtimeRevisions/2"}}
	cases := []struct {
		name     string
		traffic  *Traffic
		recorded string
		want     string
	}{
		{"always latest serves the newest", &Traffic{AlwaysLatest: true}, "", "e/runtimeRevisions/2"},
		{"manual serves the largest share", splitBetween("e/runtimeRevisions/1", "e/runtimeRevisions/2", 40), "", "e/runtimeRevisions/1"},
		{"a recorded stable revision wins", splitBetween("e/runtimeRevisions/1", "e/runtimeRevisions/2", 60), "e/runtimeRevisions/1", "e/runtimeRevisions/1"},
		{"a recorded revision with no share does not", pinnedTo("e/runtimeRevisions/2"), "e/runtimeRevisions/1", "e/runtimeRevisions/2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := servingRevision(tc.traffic, revs, tc.recorded)
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := servingRevision(&Traffic{AlwaysLatest: true}, nil, ""); err == nil {
		t.Error("an engine with no revisions has nothing to keep serving")
	}
}

func TestDescribeTrafficNamesPackVersions(t *testing.T) {
	revs := []Revision{{Name: "e/runtimeRevisions/1", PackVersion: "v1"}, {Name: "e/runtimeRevisions/2"}}
	got := describeTraffic(splitBetween("e/runtimeRevisions/1", "e/runtimeRevisions/2", 10), revs)
	if got != "1 (v1) 90%, 2 10%" {
		t.Errorf("got %q", got)
	}
	if got := describeTraffic(&Traffic{AlwaysLatest: true}, revs[:1]); got != "always latest: 1 (v1) 100%" {
		t.Errorf("got %q", got)
	}
}

func TestBuildEngineTagsThePackVersion(t *testing.T) {
	in := testApplyInput()
	in.PackVersion = "v1.2.3"
	spec, errs := buildEngine(in, AgentInfo{Name: "assistant"})
	if len(errs) != 0 {
		t.Fatalf("buildEngine: %v", errs)
	}
	if spec.Env[envPackVersion] != "v1.2.3" {
		t.Errorf("%s = %q, want v1.2.3", envPackVersion, spec.Env[envPackVersion])
	}

	in.PackVersion = ""
	spec, _ = buildEngine(in, AgentInfo{Name: "assistant"})
	if _, set := spec.Env[envPackVersion]; set {
		t.Error("a pack with no version must not set the variable")
	}
}

func TestPackVersion(t *testing.T) {
	if got := packVersion(`{"id":"x","version":"v1.0.0"}`); got != "v1.0.0" {
		t.Errorf("got %q", got)
	}
	if got := packVersion(`not json`); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestValidateRollout(t *testing.T) {
	cases := []struct {
		name    string
		stage   string
		percent *int
		wantErr string
	}{
		{"unset is live", "", nil, ""},
		{"candidate with a share", RolloutStageCandidate, intPtr(25), ""},
		{"unknown stage", "canary", nil, "rollout_stage"},
		{"share of 100", RolloutStageCandidate, intPtr(100), "from 0 to 99"},
		{"negative share", RolloutStageCandidate, intPtr(-1), "from 0 to 99"},
		{"share without candidate", RolloutStagePromote, intPtr(5), "only with rollout_stage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := (&Config{RolloutStage: tc.stage, CandidatePercent: tc.percent}).validateRollout()
			joined := strings.Join(errs, "; ")
			if tc.wantErr == "" && len(errs) != 0 {
				t.Errorf("errs = %v, want none", errs)
			}
			if tc.wantErr != "" && !strings.Contains(joined, tc.wantErr) {
				t.Errorf("errs = %q, want %q", joined, tc.wantErr)
			}
		})
	}
}

// The rollout options are about traffic, not the engine, so changing them must
// not make a plan report the config as changed.
func TestRolloutOptionsDoNotChangeTheConfigHash(t *testing.T) {
	base := &Config{Project: "p", Location: "l", Image: "i"}
	staged := &Config{Project: "p", Location: "l", Image: "i",
		RolloutStage: RolloutStageCandidate, CandidatePercent: intPtr(10)}
	a, _ := hashPlanConfig(base, nil, "")
	b, _ := hashPlanConfig(staged, nil, "")
	if a != b {
		t.Error("rollout_stage or candidate_percent changed the config hash")
	}
}

func TestPlanDescribesTheRollout(t *testing.T) {
	prior := newState()
	prior.PackHash, prior.ConfigHash = "old", "c"
	prior.Engines["assistant"] = EngineState{
		ResourceName:    "e",
		ServingRevision: "e/runtimeRevisions/4",
	}
	in := &planInput{
		Agents: []AgentInfo{{Name: "assistant"}}, Prior: prior,
		PackHash: "new", ConfigHash: "c", Delivery: PackDelivery{Inline: true},
		Stage: RolloutStageCandidate, CandidatePercent: 10,
	}
	got := buildPlan(in).Changes[0].Detail
	if !strings.Contains(got, "candidate at 10%, revision 4 keeps 90%") {
		t.Errorf("candidate detail = %q", got)
	}

	in.Stage = RolloutStagePromote
	plan := buildPlan(in)
	if plan.Changes[0].Action != deploy.ActionNoChange {
		t.Errorf("promote with nothing staged = %+v, want no change", plan.Changes[0])
	}

	entry := prior.Engines["assistant"]
	entry.CandidateRevision = "e/runtimeRevisions/5"
	prior.Engines["assistant"] = entry
	plan = buildPlan(in)
	if plan.Changes[0].Action != deploy.ActionUpdate ||
		!strings.Contains(plan.Changes[0].Detail, "Promote candidate revision 5") {
		t.Errorf("promote change = %+v", plan.Changes[0])
	}
	if len(plan.Warnings) == 0 {
		t.Error("a promote after the pack changed should warn that the staged pack is what serves")
	}
}

// failAfterUpdate fails the revision listing that follows an update.
type failAfterUpdate struct {
	*rolloutClient
	updated bool
}

func (c *failAfterUpdate) UpdateEngine(ctx context.Context, name string, spec *EngineSpec) (*Engine, error) {
	c.updated = true
	return c.rolloutClient.UpdateEngine(ctx, name, spec)
}

func (c *failAfterUpdate) ListRevisions(ctx context.Context, name string) ([]Revision, error) {
	if c.updated {
		return nil, errors.New("list failed")
	}
	return c.rolloutClient.ListRevisions(ctx, name)
}

// A candidate staged earlier is no longer what the spec describes once a new
// update has landed, so a failure after the update must drop it from state:
// otherwise the next promote serves the stale version.
func TestAFailureAfterTheUpdateDropsTheStaleCandidate(t *testing.T) {
	base := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	staged, err := applyEngines(context.Background(), base, rolloutInput(RolloutStageCandidate, nil, "v2"),
		agents, deployed(t, base), nil)
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}

	for _, stage := range []string{RolloutStageCandidate, RolloutStageLive} {
		c := &failAfterUpdate{rolloutClient: base}
		got, err := applyEngines(context.Background(), c, rolloutInput(stage, nil, "v3"), agents, staged, nil)
		if err == nil {
			t.Fatalf("%s: want the listing failure", stage)
		}
		entry := got.Engines["assistant"]
		if entry.CandidateRevision != "" || entry.ResourceName == "" {
			t.Errorf("%s: entry = %+v, want the engine kept and no candidate", stage, entry)
		}
		if got.PackHash != "" {
			t.Errorf("%s: hashes must be cleared after a partial apply", stage)
		}
	}
}

// Re-staging with a share sets the split once, after the update, so the
// candidate never drops to 0% in between.
func TestRestagingKeepsTheCandidateShare(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	first, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, intPtr(10), "v2"),
		agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	c.calls = nil
	if _, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, intPtr(10), "v3"),
		agents, first, nil); err != nil {
		t.Fatalf("second candidate: %v", err)
	}
	if want := "update,traffic:1 90%, 3 10%"; strings.Join(c.calls, ",") != want {
		t.Errorf("calls = %v, want %q", c.calls, want)
	}
}

func TestNewestSkipsRevisionsThatCannotServe(t *testing.T) {
	revs := []Revision{{Name: "e/runtimeRevisions/1", State: "ACTIVE"}, {Name: "e/runtimeRevisions/2", State: "FAILED"}}
	got, err := servingRevision(&Traffic{AlwaysLatest: true}, revs, "")
	if err != nil || got != "e/runtimeRevisions/1" {
		t.Errorf("got %q, %v; want the active revision", got, err)
	}
}

// Each dry run starts empty, so promote must work on an engine known only from
// prior state.
func TestDryRunPromote(t *testing.T) {
	prior := newState()
	prior.Engines["assistant"] = EngineState{ResourceName: "e", CandidateRevision: "e/runtimeRevisions/2"}
	got, err := applyEngines(context.Background(), newDryRunClient(&Config{}),
		rolloutInput(RolloutStagePromote, nil, ""), []AgentInfo{{Name: "assistant"}}, prior, nil)
	if err != nil || got.Engines["assistant"].ServingRevision != "e/runtimeRevisions/2" {
		t.Errorf("got %+v, %v", got.Engines, err)
	}
}

func TestPlanShowsALiveApplyServingAStagedCandidate(t *testing.T) {
	prior := newState()
	prior.PackHash, prior.ConfigHash = "p", "c"
	prior.Engines["assistant"] = EngineState{ResourceName: "e", CandidateRevision: "e/runtimeRevisions/5"}
	in := &planInput{
		Agents: []AgentInfo{{Name: "assistant"}}, Prior: prior,
		PackHash: "p", ConfigHash: "c", Delivery: PackDelivery{Inline: true}, Stage: RolloutStageLive,
	}
	change := buildPlan(in).Changes[0]
	if change.Action != deploy.ActionUpdate || !strings.Contains(change.Detail, "serves it at 100%") {
		t.Errorf("change = %+v", change)
	}
}

// Agent Runtime creates a revision on every spec update, identical or not, so
// re-staging an unchanged candidate must only set its share.
func TestRestagingAnUnchangedCandidateSendsNoSpec(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	in := rolloutInput(RolloutStageCandidate, nil, "v2")
	in.PackHash, in.ConfigHash = "p2", "c"
	staged, err := applyEngines(context.Background(), c, in, agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	c.calls = nil

	in.Cfg.CandidatePercent = intPtr(10)
	got, err := applyEngines(context.Background(), c, in, agents, staged, nil)
	if err != nil {
		t.Fatalf("restage: %v", err)
	}
	if strings.Join(c.calls, ",") != "traffic:1 90%, 2 10%" {
		t.Errorf("calls = %v, want the share alone", c.calls)
	}
	if revisionID(got.Engines["assistant"].CandidateRevision) != "2" {
		t.Errorf("entry = %+v, want candidate 2 kept", got.Engines["assistant"])
	}
}

// A candidate replaced by a newer one has no traffic but keeps a warm
// instance, so it is deleted.
func TestAReplacedCandidateIsDeleted(t *testing.T) {
	c := newRolloutClient()
	agents := []AgentInfo{{Name: "assistant"}}
	first, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v2"),
		agents, deployed(t, c), nil)
	if err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	engine := first.Engines["assistant"].ResourceName
	if _, err := applyEngines(context.Background(), c, rolloutInput(RolloutStageCandidate, nil, "v3"),
		agents, first, nil); err != nil {
		t.Fatalf("second candidate: %v", err)
	}
	revs, _ := c.ListRevisions(context.Background(), engine)
	for _, r := range revs {
		if revisionID(r.Name) == "2" {
			t.Error("the replaced candidate revision 2 is still there")
		}
	}
	if len(revs) != 2 {
		t.Errorf("revisions = %v, want the stable 1 and the candidate 3", revs)
	}
}
