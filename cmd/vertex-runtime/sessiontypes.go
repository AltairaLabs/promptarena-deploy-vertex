package main

import (
	"context"

	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"github.com/googleapis/gax-go/v2"
)

// gaxCallOption is the generated client's per-call option type, named here so
// the store's interface matches it without every file importing gax.
type gaxCallOption = gax.CallOption

// eventIterator is the part of the generated event iterator the store uses.
type eventIterator interface {
	Next() (*aiplatformpb.SessionEvent, error)
}

// createSessionOp is the long-running create the store waits on.
type createSessionOp interface {
	Wait(ctx context.Context, opts ...gaxCallOption) (*aiplatformpb.Session, error)
}
