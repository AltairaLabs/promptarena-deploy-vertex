package vertex

import (
	"context"
	"errors"
)

// Engine state values reported by Agent Runtime.
const (
	// EngineStateActive means the engine is serving.
	EngineStateActive = "ACTIVE"
	// EngineStateCreating means creation is still in progress.
	EngineStateCreating = "CREATING"
	// EngineStateUnknown covers any state this adapter does not model.
	EngineStateUnknown = "UNKNOWN"
)

// ErrEngineNotFound is returned when an engine does not exist. Callers use
// errors.Is rather than matching on message text.
var ErrEngineNotFound = errors.New("vertex: reasoning engine not found")

// Engine is the adapter's view of a deployed Agent Runtime engine.
type Engine struct {
	// ResourceName is the full projects/…/reasoningEngines/… name.
	ResourceName string
	// DisplayName is the human-facing name, set from the agent name.
	DisplayName string
	// Labels are the resource labels, including the managed promptkit-* keys.
	Labels map[string]string
	// State is one of the EngineState* values.
	State string
}

// EngineSpec is the adapter's desired-state description of one engine. The
// client translates it into the provider's proto; keeping the adapter's own
// shape here means buildEngine is testable without the GCP SDK.
type EngineSpec struct {
	DisplayName    string
	Description    string
	ImageURI       string
	ServiceAccount string
	Labels         map[string]string
	// Env becomes deploymentSpec.env, which the API models as a list of
	// name/value pairs; the client performs that conversion in sorted key
	// order so requests are deterministic.
	Env                  map[string]string
	ResourceLimits       map[string]string
	MinInstances         *int
	MaxInstances         *int
	ContainerConcurrency *int
	// AgentCard becomes spec.agentCard, a free-form A2A Agent Card object.
	AgentCard map[string]any
}

// gcpClient abstracts the Agent Runtime control plane.
type gcpClient interface {
	// CreateEngine creates an engine and waits for the operation to finish.
	CreateEngine(ctx context.Context, spec *EngineSpec) (*Engine, error)
	// UpdateEngine patches an existing engine and waits for completion,
	// returning a wrapped ErrEngineNotFound when it does not exist.
	UpdateEngine(ctx context.Context, name string, spec *EngineSpec) (*Engine, error)
	// GetEngine fetches one engine, returning a wrapped ErrEngineNotFound
	// when it does not exist.
	GetEngine(ctx context.Context, name string) (*Engine, error)
	// DeleteEngine removes an engine and waits for completion. Deleting an
	// engine that is already gone is not an error.
	DeleteEngine(ctx context.Context, name string) error
	// ListEnginesByLabel returns engines whose labels are a superset of the
	// selector. A nil or empty selector matches every engine.
	ListEnginesByLabel(
		ctx context.Context, project, location string, labels map[string]string,
	) ([]Engine, error)
}
