package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
)

// lifecycleClient records deletes and can fail a chosen engine, which is what
// the partial-failure cases need.
type lifecycleClient struct {
	gcpClient // unused methods panic if called, which is what we want

	existing  map[string]bool
	deletes   []string
	failOn    map[string]error
	getFailed error
}

func (c *lifecycleClient) DeleteEngine(_ context.Context, name string) error {
	c.deletes = append(c.deletes, name)
	if err, ok := c.failOn[name]; ok {
		return err
	}
	return nil
}

func (c *lifecycleClient) GetEngine(_ context.Context, name string) (*Engine, error) {
	if c.getFailed != nil {
		return nil, c.getFailed
	}
	if !c.existing[name] {
		return nil, ErrEngineNotFound
	}
	return &Engine{ResourceName: name, State: EngineStateActive}, nil
}

func lifecycleProvider(c gcpClient) *Provider {
	p := NewProvider()
	p.clientFunc = func(context.Context, *Config) (gcpClient, error) { return c, nil }
	return p
}

// engineResourceName mirrors the path stateWithEngines records, since a bare
// id is not addressable.
func engineResourceName(name string) string {
	return "projects/p/locations/l/reasoningEngines/" + name
}

// lifecycleConfig is the deploy config Destroy and Status parse. They need
// only the fields that identify the project, so this stays smaller than the
// plan fixture.
func lifecycleConfig() string {
	return `{
		"project":"my-project",
		"location":"us-central1",
		"image":"us-central1-docker.pkg.dev/my-project/r/i",
		"providers":[{"name":"default","role":"llm","type":"gemini","model":"gemini-2.5-flash"}]
	}`
}

func marshalState(t *testing.T, s *State) string {
	t.Helper()
	encoded, err := s.Marshal()
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return encoded
}

func stateWithNamedEngines(t *testing.T, names ...string) string {
	t.Helper()
	return marshalState(t, stateWithEngines(names...))
}

// --- Destroy ---------------------------------------------------------------

func TestDestroy_DeletesEveryEngine(t *testing.T) {
	client := &lifecycleClient{}
	var events []*deploy.DestroyEvent

	err := lifecycleProvider(client).Destroy(context.Background(), &deploy.DestroyRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "beta", "alpha"),
	}, func(e *deploy.DestroyEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	if len(client.deletes) != 2 {
		t.Fatalf("deleted %v, want both engines", client.deletes)
	}
	// Stable order: destroy output should not churn between runs.
	if client.deletes[0] != engineResourceName("alpha") {
		t.Errorf("deleted %v, want alpha first", client.deletes)
	}
	if countEvents(events, "resource") != 2 {
		t.Errorf("events = %+v, want a resource event per engine", events)
	}
}

// Destroy has to converge on an already-clean project, or a retried teardown
// after a partial failure becomes manual work.
func TestDestroy_AlreadyGoneIsSuccess(t *testing.T) {
	client := &lifecycleClient{}

	err := lifecycleProvider(client).Destroy(context.Background(), &deploy.DestroyRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "solo"),
	}, nil)
	if err != nil {
		t.Errorf("deleting an engine that is already gone must succeed, got %v", err)
	}
}

func TestDestroy_EmptyStateIsANoOp(t *testing.T) {
	client := &lifecycleClient{}

	err := lifecycleProvider(client).Destroy(context.Background(), &deploy.DestroyRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   "",
	}, nil)
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if len(client.deletes) != 0 {
		t.Errorf("deleted %v with nothing in state", client.deletes)
	}
}

// Stopping at the first failure would strand the remaining engines — each one
// billing while it runs — and leave the operator to work out which survived.
func TestDestroy_ContinuesPastAFailure(t *testing.T) {
	client := &lifecycleClient{failOn: map[string]error{
		engineResourceName("alpha"): errors.New("permission denied"),
	}}
	var events []*deploy.DestroyEvent

	err := lifecycleProvider(client).Destroy(context.Background(), &deploy.DestroyRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "alpha", "beta"),
	}, func(e *deploy.DestroyEvent) error { events = append(events, e); return nil })

	if err == nil {
		t.Error("a failed delete must surface, not be swallowed")
	}
	if len(client.deletes) != 2 {
		t.Errorf("deleted %v, want the second engine attempted despite the first failing",
			client.deletes)
	}
	if countEvents(events, "error") == 0 {
		t.Errorf("events = %+v, want the failure reported", events)
	}
}

// An engine recorded mid-creation has no resource name to address. Skipping it
// silently would leave something possibly running with no mention of it.
func TestDestroy_ReportsEnginesWithNoResourceName(t *testing.T) {
	s := newState()
	s.Engines["starting"] = EngineState{InFlight: true}
	client := &lifecycleClient{}
	var events []*deploy.DestroyEvent

	err := lifecycleProvider(client).Destroy(context.Background(), &deploy.DestroyRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   marshalState(t, s),
	}, func(e *deploy.DestroyEvent) error { events = append(events, e); return nil })
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}

	if len(client.deletes) != 0 {
		t.Errorf("deleted %v, but there was no resource name to delete", client.deletes)
	}
	if countEvents(events, "error") == 0 {
		t.Errorf("events = %+v, want the un-deletable engine reported", events)
	}
}

// --- Status ----------------------------------------------------------------

func TestStatus_ReportsDeployedWhenEveryEngineIsActive(t *testing.T) {
	client := &lifecycleClient{existing: map[string]bool{
		engineResourceName("alpha"): true,
		engineResourceName("beta"):  true,
	}}

	got, err := lifecycleProvider(client).Status(context.Background(), &deploy.StatusRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "alpha", "beta"),
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if got.Status != StatusDeployed {
		t.Errorf("Status = %q, want %q (%+v)", got.Status, StatusDeployed, got.Resources)
	}
	if len(got.Resources) != 2 {
		t.Fatalf("resources = %+v, want one per engine", got.Resources)
	}
	if len(got.Resources[0].Links) == 0 {
		t.Error("a healthy engine should carry the endpoint link")
	}
}

func TestStatus_NothingDeployed(t *testing.T) {
	got, err := lifecycleProvider(&lifecycleClient{}).Status(
		context.Background(), &deploy.StatusRequest{
			DeployConfig: lifecycleConfig(),
			PriorState:   "",
		})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Status != StatusNotDeployed {
		t.Errorf("Status = %q, want %q", got.Status, StatusNotDeployed)
	}
}

// An engine deleted out of band is drift, and reporting it is the point.
func TestStatus_MissingEngineDegradesTheDeployment(t *testing.T) {
	client := &lifecycleClient{existing: map[string]bool{
		engineResourceName("alpha"): true,
	}}

	got, err := lifecycleProvider(client).Status(context.Background(), &deploy.StatusRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "alpha", "gone"),
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	if got.Status != StatusDegraded {
		t.Errorf("Status = %q, want %q", got.Status, StatusDegraded)
	}
	missing := findResource(got.Resources, "gone")
	if missing == nil || missing.Status != ResourceMissing {
		t.Fatalf("resource for the deleted engine = %+v, want %q", missing, ResourceMissing)
	}
	// Nothing to call, so the link would be a promise the deployment cannot keep.
	if len(missing.Links) != 0 {
		t.Errorf("a missing engine must carry no endpoint link, got %+v", missing.Links)
	}
}

// A failed lookup is not evidence of absence — the same rule the drift contract
// applies. Reporting it as missing would read as "it is gone" when we simply
// could not tell.
func TestStatus_LookupFailureIsUnhealthyNotMissing(t *testing.T) {
	client := &lifecycleClient{getFailed: errors.New("deadline exceeded")}

	got, err := lifecycleProvider(client).Status(context.Background(), &deploy.StatusRequest{
		DeployConfig: lifecycleConfig(),
		PriorState:   stateWithNamedEngines(t, "alpha"),
	})
	if err != nil {
		t.Fatalf("Status must not fail when a lookup does: %v", err)
	}

	res := findResource(got.Resources, "alpha")
	if res == nil || res.Status != ResourceUnhealthy {
		t.Fatalf("resource = %+v, want %q", res, ResourceUnhealthy)
	}
	if !strings.Contains(res.Detail, "Could not verify") {
		t.Errorf("Detail = %q, want it to say the lookup failed", res.Detail)
	}
}

func countEvents(events []*deploy.DestroyEvent, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

func findResource(resources []deploy.ResourceStatus, name string) *deploy.ResourceStatus {
	for i := range resources {
		if resources[i].Name == name {
			return &resources[i]
		}
	}
	return nil
}
