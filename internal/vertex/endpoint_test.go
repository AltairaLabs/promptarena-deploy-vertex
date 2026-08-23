package vertex

import (
	"testing"
)

const testEngineName = "projects/p/locations/us-central1/reasoningEngines/123"

// Pinned to a literal: this is the URL a user copies out of a deploy and runs,
// and it matches what the deployed integration tests call.
func TestEngineURL(t *testing.T) {
	got := engineURL("us-central1", testEngineName)
	want := "https://us-central1-aiplatform.googleapis.com/v1beta1/" + testEngineName + ":query"

	if got != want {
		t.Errorf("engineURL =\n  %q\nwant\n  %q", got, want)
	}
}

// The regional host is load-bearing. The global aiplatform endpoint does not
// serve reasoning engines, so a URL built without the location prefix resolves
// and then 404s — the worst kind of wrong link.
func TestEngineURLCarriesTheRegionalHost(t *testing.T) {
	got := engineURL("europe-west4", testEngineName)
	if got == "" {
		t.Fatal("engineURL returned nothing for a valid input")
	}
	if got[:len("https://europe-west4-")] != "https://europe-west4-" {
		t.Errorf("engineURL = %q, want it to lead with the regional host", got)
	}
}

func TestEngineURLNeedsEveryPart(t *testing.T) {
	tests := []struct {
		name     string
		location string
		resource string
	}{
		{"no location", "", testEngineName},
		{"no resource name", "us-central1", ""},
		// A bare id is not addressable: the full path carries the project and
		// location the URL needs.
		{"resource name is not a full path", "us-central1", "123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := engineURL(tt.location, tt.resource); got != "" {
				t.Errorf("engineURL = %q, want empty", got)
			}
			if links := endpointLinks(tt.location, tt.resource); links != nil {
				t.Errorf("endpointLinks = %+v, want nil", links)
			}
		})
	}
}

func TestEndpointLinksShape(t *testing.T) {
	links := endpointLinks("us-central1", testEngineName)
	if len(links) != 1 {
		t.Fatalf("endpointLinks = %+v, want exactly one link", links)
	}
	if links[0].Rel != "endpoint" {
		t.Errorf("Rel = %q, want endpoint", links[0].Rel)
	}
	if links[0].Label == "" {
		t.Error("a link with no label renders as a bare URL")
	}
}
