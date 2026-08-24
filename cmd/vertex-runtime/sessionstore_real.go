package main

// Everything here is glue over the live Agent Runtime session client: each
// method forwards one call, and openSessionStore builds the client. None of it
// can run without a project, so this file is excluded from coverage the way
// storage_real.go is, and kept separate so the exclusion names it alone.

import (
	"context"
	"fmt"
	"log/slog"

	aiplatform "cloud.google.com/go/aiplatform/apiv1beta1"
	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
)

// realSessionClient adapts the generated client to sessionClient.
type realSessionClient struct {
	c *aiplatform.SessionClient
}

func (r realSessionClient) GetSession(
	ctx context.Context, req *aiplatformpb.GetSessionRequest, opts ...gaxCallOption,
) (*aiplatformpb.Session, error) {
	return r.c.GetSession(ctx, req)
}

func (r realSessionClient) ListEvents(
	ctx context.Context, req *aiplatformpb.ListEventsRequest, opts ...gaxCallOption,
) eventIterator {
	return r.c.ListEvents(ctx, req)
}

func (r realSessionClient) AppendEvent(
	ctx context.Context, req *aiplatformpb.AppendEventRequest, opts ...gaxCallOption,
) (*aiplatformpb.AppendEventResponse, error) {
	return r.c.AppendEvent(ctx, req)
}

func (r realSessionClient) CreateSession(
	ctx context.Context, req *aiplatformpb.CreateSessionRequest, opts ...gaxCallOption,
) (createSessionOp, error) {
	return r.c.CreateSession(ctx, req)
}

// openSessionStore builds the store when this runtime knows which engine it is.
//
// Agent Runtime tells a deployed agent its engine id; nothing tells it the full
// resource name, and outside a deployment there is no engine at all. A nil
// store means requests naming a session are refused rather than silently
// answered without one.
func openSessionStore(
	ctx context.Context, cfg *runtimeConfig, log *slog.Logger,
) (*SessionStore, error) {
	engine := cfg.engineName()
	if engine == "" {
		log.Info("no session storage: conversations will not carry between requests",
			"engine_id", cfg.EngineID, "project", cfg.Project, "location", cfg.Location)
		return nil, nil
	}

	client, err := aiplatform.NewSessionClient(ctx,
		option.WithEndpoint(fmt.Sprintf("%s-aiplatform.googleapis.com:443", cfg.Location)))
	if err != nil {
		return nil, fmt.Errorf("session client: %w", err)
	}

	log.Info("session storage configured", "engine", engine)
	return NewSessionStore(realSessionClient{c: client}, engine), nil
}
