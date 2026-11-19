package vertex

import "errors"

// ErrNotImplemented is returned by lifecycle methods that are not yet
// implemented in this phase.
var ErrNotImplemented = errors.New(
	"vertex: not implemented in this adapter version")

// engineCreatingError reports a create the provider accepted but that did not
// finish: the wait failed or was interrupted. The engine may exist or still be
// provisioning under ResourceName, so apply records it as in-flight rather than
// forgetting it.
type engineCreatingError struct {
	ResourceName string
	err          error
}

func (e *engineCreatingError) Error() string { return e.err.Error() }

func (e *engineCreatingError) Unwrap() error { return e.err }
