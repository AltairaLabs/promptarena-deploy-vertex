package vertex

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/promptarena/deploy"
	"github.com/AltairaLabs/promptarena/deploy/adaptersdk"
)

// queryMethod is the unary method an engine serves. streamQuery is the
// streaming counterpart, but one link is enough to get started and two would
// bury the one most callers want.
const queryMethod = "query"

// engineURL builds the address a caller POSTs a turn to.
//
// The engine's own resource name already carries the project and location, so
// only the regional host has to be derived. That host is not optional: the
// global aiplatform endpoint does not serve reasoning engines, so a link built
// without the location prefix resolves and then 404s, which is worse than not
// linking at all.
//
// This is the same URL the deployed integration tests call, which is what makes
// it safe for the adapter to construct: it is this adapter's own route, not a
// guess at someone else's console layout.
func engineURL(location, resourceName string) string {
	if location == "" || resourceName == "" {
		return ""
	}
	// A resource name must be the full projects/…/reasoningEngines/… path;
	// anything shorter would build a URL that cannot resolve.
	if !strings.HasPrefix(resourceName, "projects/") {
		return ""
	}
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1beta1/%s:%s",
		location, resourceName, queryMethod)
}

// endpointLinks wraps the engine URL as resource links, or nil when any part is
// unknown. An absent link beats one that does not resolve.
func endpointLinks(location, resourceName string) []deploy.ResourceLink {
	return adaptersdk.Link("Query endpoint", engineURL(location, resourceName), "endpoint")
}
