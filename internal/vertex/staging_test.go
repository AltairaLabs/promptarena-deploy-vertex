package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// stagingClient records what was uploaded so the tests can tell a real upload
// from a skipped one.
type stagingClient struct {
	gcpClient // unused methods panic if called, which is what we want

	staged  map[string][]byte
	failErr error
}

func (c *stagingClient) StageObject(_ context.Context, uri string, data []byte) error {
	if c.failErr != nil {
		return c.failErr
	}
	if c.staged == nil {
		c.staged = map[string][]byte{}
	}
	c.staged[uri] = data
	return nil
}

func stagingInput(inline bool, bucket string) *engineInput {
	return &engineInput{
		Cfg:      &Config{StagingBucket: bucket},
		PackJSON: `{"id":"demo"}`,
		PackHash: "abc123",
		Delivery: PackDelivery{Inline: inline, SizeBytes: 30000},
	}
}

// An inline pack rides in the environment, so there is no object to write.
func TestStagePack_InlineStagesNothing(t *testing.T) {
	client := &stagingClient{}

	uri, err := stagePack(context.Background(), client, stagingInput(true, "gs://b"), nil)
	if err != nil {
		t.Fatalf("stagePack: %v", err)
	}
	if uri != "" {
		t.Errorf("uri = %q, want empty for an inline pack", uri)
	}
	if len(client.staged) != 0 {
		t.Errorf("staged %v, want nothing uploaded", client.staged)
	}
}

func TestStagePack_UploadsAndReturnsTheURI(t *testing.T) {
	client := &stagingClient{}
	in := stagingInput(false, "gs://b")

	uri, err := stagePack(context.Background(), client, in, nil)
	if err != nil {
		t.Fatalf("stagePack: %v", err)
	}

	if uri != "gs://b/packs/abc123/pack.json" {
		t.Errorf("uri = %q, want it keyed by pack hash under the bucket", uri)
	}
	if string(client.staged[uri]) != in.PackJSON {
		t.Errorf("staged %q, want the pack bytes", client.staged[uri])
	}
}

// The object is keyed by hash, so state recording this exact URI means the
// bytes at the far end are already these bytes. Re-uploading is a wasted
// round trip and a wasted write.
func TestStagePack_UnchangedPackIsNotReuploaded(t *testing.T) {
	client := &stagingClient{}
	in := stagingInput(false, "gs://b")
	prior := &State{StagedPackURI: "gs://b/packs/abc123/pack.json"}

	uri, err := stagePack(context.Background(), client, in, prior)
	if err != nil {
		t.Fatalf("stagePack: %v", err)
	}
	if uri != prior.StagedPackURI {
		t.Errorf("uri = %q, want the URI already in state", uri)
	}
	if len(client.staged) != 0 {
		t.Errorf("staged %v, want no upload for an unchanged pack", client.staged)
	}
}

// A changed pack hashes differently, so it lands at a new object rather than
// overwriting the one a running engine may still be reading.
func TestStagePack_ChangedPackGetsANewObject(t *testing.T) {
	client := &stagingClient{}
	in := stagingInput(false, "gs://b")
	in.PackHash = "def456"
	prior := &State{StagedPackURI: "gs://b/packs/abc123/pack.json"}

	uri, err := stagePack(context.Background(), client, in, prior)
	if err != nil {
		t.Fatalf("stagePack: %v", err)
	}
	if uri == prior.StagedPackURI {
		t.Fatal("a changed pack must not reuse the previous object")
	}
	if len(client.staged) != 1 {
		t.Errorf("staged %v, want the new pack uploaded", client.staged)
	}
}

// A failed upload must stop the deploy: the engine would otherwise be pointed
// at an object that is not there, and fail at startup instead of at apply.
func TestStagePack_UploadFailureIsFatal(t *testing.T) {
	client := &stagingClient{failErr: errors.New("permission denied")}

	_, err := stagePack(context.Background(), client, stagingInput(false, "gs://b"), nil)
	if err == nil {
		t.Fatal("a failed upload must surface")
	}
	if !strings.Contains(err.Error(), "stage pack") {
		t.Errorf("err = %v, want it to say staging failed", err)
	}
}

func TestValidatePackDelivery(t *testing.T) {
	tests := []struct {
		name    string
		inline  bool
		bucket  string
		wantErr bool
	}{
		{"inline needs no bucket", true, "", false},
		{"staged with a bucket is fine", false, "gs://b", false},
		{"staged with no bucket is rejected", false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackDelivery(
				PackDelivery{Inline: tt.inline, SizeBytes: 30000},
				&Config{StagingBucket: tt.bucket})
			if tt.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// The URI this writes is the URI the runtime reads, so the two must agree.
// parseGCSURI in cmd/vertex-runtime is the other half of this contract.
func TestStagedPackURIShape(t *testing.T) {
	got := stagedPackURI("gs://bucket", "hash")
	if got != "gs://bucket/packs/hash/pack.json" {
		t.Errorf("stagedPackURI = %q", got)
	}
	// A trailing slash on the bucket must not double the separator.
	if got := stagedPackURI("gs://bucket/", "hash"); strings.Contains(got, "//packs") {
		t.Errorf("stagedPackURI = %q, want no doubled separator", got)
	}
	if got := stagedPackURI("", "hash"); got != "" {
		t.Errorf("stagedPackURI = %q, want empty without a bucket", got)
	}
}

// splitGCSURI is the other half of a contract: the URI this adapter writes is
// the URI cmd/vertex-runtime reads with parseGCSURI. These cases mirror that
// function's, so a change to either shape fails here rather than at an engine's
// first startup.
func TestSplitGCSURI(t *testing.T) {
	t.Run("splits a valid URI", func(t *testing.T) {
		bucket, object, err := splitGCSURI("gs://my-bucket/packs/abc/pack.json")
		if err != nil {
			t.Fatalf("splitGCSURI: %v", err)
		}
		if bucket != "my-bucket" {
			t.Errorf("bucket = %q, want my-bucket", bucket)
		}
		// The object keeps every segment after the bucket; splitting on the
		// last slash instead would address the wrong object.
		if object != "packs/abc/pack.json" {
			t.Errorf("object = %q, want the full object path", object)
		}
	})

	rejects := []struct {
		name string
		uri  string
	}{
		{"no scheme", "my-bucket/pack.json"},
		{"wrong scheme", "s3://my-bucket/pack.json"},
		{"bucket only", "gs://my-bucket"},
		{"empty bucket", "gs:///pack.json"},
		{"empty object", "gs://my-bucket/"},
		{"empty", ""},
	}
	for _, tt := range rejects {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := splitGCSURI(tt.uri); err == nil {
				t.Errorf("splitGCSURI(%q) accepted an unusable URI", tt.uri)
			}
		})
	}
}

// What stagedPackURI builds must be what splitGCSURI can take apart. If these
// two ever disagree the adapter uploads to one place and tells the engine to
// read from another.
func TestStagedURIRoundTrips(t *testing.T) {
	uri := stagedPackURI("gs://bucket", "hash")

	bucket, object, err := splitGCSURI(uri)
	if err != nil {
		t.Fatalf("the URI this adapter builds must be one it can split: %v", err)
	}
	if bucket != "bucket" {
		t.Errorf("bucket = %q, want bucket", bucket)
	}
	if object != stagedPackObject("hash") {
		t.Errorf("object = %q, want %q", object, stagedPackObject("hash"))
	}
}
