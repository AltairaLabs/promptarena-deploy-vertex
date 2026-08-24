package vertex

import (
	"context"
	"fmt"
	"math"
	"sort"

	aiplatform "cloud.google.com/go/aiplatform/apiv1beta1"
	aiplatformpb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// realClient talks to Agent Runtime over the v1beta1 API.
//
// v1beta1 rather than v1 because the A2A agent card lives there. Note that the
// generated Go protos do not yet expose spec.agent_card even in v1beta1 — the
// field is present in the REST discovery document but absent from the published
// protos in googleapis/googleapis, so A2A cards cannot be set through this
// client. See the adapter's README for the follow-up.
type realClient struct {
	client   *aiplatform.ReasoningEngineClient
	project  string
	location string
}

// newRealClient dials the regional Agent Runtime endpoint using Application
// Default Credentials.
func newRealClient(ctx context.Context, cfg *Config) (gcpClient, error) {
	endpoint := fmt.Sprintf("%s-aiplatform.googleapis.com:443", cfg.Location)

	client, err := aiplatform.NewReasoningEngineClient(ctx, option.WithEndpoint(endpoint))
	if err != nil {
		return nil, fmt.Errorf("create reasoning engine client: %w", err)
	}

	return &realClient{
		client:   client,
		project:  cfg.Project,
		location: cfg.Location,
	}, nil
}

// parent is the collection path engines are created under.
func (c *realClient) parent() string {
	return fmt.Sprintf("projects/%s/locations/%s", c.project, c.location)
}

// CreateEngine creates an engine and waits for the long-running operation.
func (c *realClient) CreateEngine(ctx context.Context, spec *EngineSpec) (*Engine, error) {
	op, err := c.client.CreateReasoningEngine(ctx, &aiplatformpb.CreateReasoningEngineRequest{
		Parent:          c.parent(),
		ReasoningEngine: specToProto(spec),
	})
	if err != nil {
		return nil, fmt.Errorf("create engine %q: %w", spec.DisplayName, err)
	}

	created, err := op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for engine %q to be created: %w", spec.DisplayName, err)
	}
	return engineFromProto(created), nil
}

// UpdateEngine patches an existing engine and waits for completion.
func (c *realClient) UpdateEngine(
	ctx context.Context, name string, spec *EngineSpec,
) (*Engine, error) {
	engine := specToProto(spec)
	engine.Name = name

	op, err := c.client.UpdateReasoningEngine(ctx, &aiplatformpb.UpdateReasoningEngineRequest{
		ReasoningEngine: engine,
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name", "description", "labels", "spec"},
		},
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%s: %w", name, ErrEngineNotFound)
		}
		return nil, fmt.Errorf("update engine %q: %w", name, err)
	}

	updated, err := op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("waiting for engine %q to update: %w", name, err)
	}
	return engineFromProto(updated), nil
}

// GetEngine fetches one engine.
func (c *realClient) GetEngine(ctx context.Context, name string) (*Engine, error) {
	got, err := c.client.GetReasoningEngine(ctx, &aiplatformpb.GetReasoningEngineRequest{
		Name: name,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, fmt.Errorf("%s: %w", name, ErrEngineNotFound)
		}
		return nil, fmt.Errorf("get engine %q: %w", name, err)
	}
	return engineFromProto(got), nil
}

// DeleteEngine removes an engine. An engine that is already gone is not an
// error, which keeps destroy idempotent.
func (c *realClient) DeleteEngine(ctx context.Context, name string) error {
	// Force, because an engine that has served a conversation owns sessions,
	// and the API refuses to delete a parent with children. Without it a
	// destroy fails on exactly the engines that were used, leaving them
	// running and billing.
	op, err := c.client.DeleteReasoningEngine(ctx, &aiplatformpb.DeleteReasoningEngineRequest{
		Name:  name,
		Force: true,
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return fmt.Errorf("delete engine %q: %w", name, err)
	}

	if waitErr := op.Wait(ctx); waitErr != nil {
		return fmt.Errorf("waiting for engine %q to delete: %w", name, waitErr)
	}
	return nil
}

// ListEnginesByLabel lists engines and filters them on labels client-side.
func (c *realClient) ListEnginesByLabel(
	ctx context.Context, project, location string, labels map[string]string,
) ([]Engine, error) {
	parent := fmt.Sprintf("projects/%s/locations/%s", project, location)
	it := c.client.ListReasoningEngines(ctx, &aiplatformpb.ListReasoningEnginesRequest{
		Parent: parent,
	})

	var out []Engine
	for {
		item, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("list engines in %s: %w", parent, err)
		}
		engine := engineFromProto(item)
		if labelsMatch(engine.Labels, labels) {
			out = append(out, *engine)
		}
	}
	return out, nil
}

// isNotFound reports whether err is a gRPC NOT_FOUND.
func isNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

// engineFromProto converts the API's engine into the adapter's view. An engine
// the API returns is serving, so state is reported as ACTIVE.
func engineFromProto(engine *aiplatformpb.ReasoningEngine) *Engine {
	return &Engine{
		ResourceName: engine.GetName(),
		DisplayName:  engine.GetDisplayName(),
		Labels:       engine.GetLabels(),
		State:        EngineStateActive,
	}
}

// specToProto converts the adapter's desired state into the API's engine.
//
// The container image goes through the deployment_source oneof rather than a
// plain field: ReasoningEngineSpec models package, source-code and container
// deployment as mutually exclusive alternatives.
func specToProto(spec *EngineSpec) *aiplatformpb.ReasoningEngine {
	engineSpec := &aiplatformpb.ReasoningEngineSpec{
		DeploymentSource: &aiplatformpb.ReasoningEngineSpec_ContainerSpec_{
			ContainerSpec: &aiplatformpb.ReasoningEngineSpec_ContainerSpec{
				ImageUri: spec.ImageURI,
			},
		},
	}

	if spec.ServiceAccount != "" {
		account := spec.ServiceAccount
		engineSpec.ServiceAccount = &account
	}

	if deployment := deploymentSpecFrom(spec); deployment != nil {
		engineSpec.DeploymentSpec = deployment
	}

	return &aiplatformpb.ReasoningEngine{
		DisplayName: spec.DisplayName,
		Description: spec.Description,
		Labels:      spec.Labels,
		Spec:        engineSpec,
	}
}

// deploymentSpecFrom builds the deployment spec, or nil when nothing is set so
// the API's own defaults apply.
//
// EngineSpec.ContainerConcurrency is deliberately not mapped: the published
// protos have no such field, even though the REST schema documents one.
func deploymentSpecFrom(spec *EngineSpec) *aiplatformpb.ReasoningEngineSpec_DeploymentSpec {
	if len(spec.Env) == 0 && len(spec.ResourceLimits) == 0 &&
		spec.MinInstances == nil && spec.MaxInstances == nil {
		return nil
	}

	out := &aiplatformpb.ReasoningEngineSpec_DeploymentSpec{
		ResourceLimits: spec.ResourceLimits,
	}

	names := make([]string, 0, len(spec.Env))
	for name := range spec.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.Env = append(out.Env, &aiplatformpb.EnvVar{
			Name:  name,
			Value: spec.Env[name],
		})
	}

	if v, ok := toInt32(spec.MinInstances); ok {
		out.MinInstances = &v
	}
	if v, ok := toInt32(spec.MaxInstances); ok {
		out.MaxInstances = &v
	}

	return out
}

// toInt32 narrows an optional int for the proto, reporting false when the value
// is absent or outside int32. Config validation bounds these well below the
// limit, so an out-of-range value means a caller bypassed validation — dropping
// it is safer than silently wrapping to a negative instance count.
func toInt32(v *int) (int32, bool) {
	if v == nil {
		return 0, false
	}
	if *v < math.MinInt32 || *v > math.MaxInt32 {
		return 0, false
	}
	return int32(*v), true
}
