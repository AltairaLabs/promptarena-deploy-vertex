package vertex

import "errors"

// ErrNotImplemented is returned by lifecycle methods that are not yet
// implemented in this phase.
var ErrNotImplemented = errors.New(
	"vertex: not implemented in this adapter version")
