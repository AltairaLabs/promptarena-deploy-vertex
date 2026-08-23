package vertex

// This file holds the one thing in the adapter that cannot be unit tested: the
// live Cloud Storage write. Everything around it — the URI it is given, the
// decision to stage at all, the skip on an unchanged pack — lives in
// staging.go and is fully covered.
//
// It is a file of its own so the coverage exclusion can name exactly this and
// nothing else. Excluding client_real.go wholesale would also stop measuring
// the pure conversion helpers in it, which are covered and worth keeping
// honest.

import (
	"context"
	"fmt"

	"cloud.google.com/go/storage"
)

// StageObject writes data to a gs://bucket/object URI using Application
// Default Credentials.
//
// The GCS client is created per call rather than held on realClient. Staging
// happens at most once per apply and only for packs over the inline limit, so
// a connection kept open for every deploy would idle through the common case.
//
// The writer is closed explicitly rather than only deferred: for GCS the close
// is what commits the object and reports a failed upload, so a deferred-only
// close would drop the error and report success for a pack that never landed.
func (c *realClient) StageObject(ctx context.Context, uri string, data []byte) error {
	bucket, object, err := splitGCSURI(uri)
	if err != nil {
		return err
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("create storage client: %w", err)
	}
	defer func() { _ = client.Close() }()

	w := client.Bucket(bucket).Object(object).NewWriter(ctx)
	w.ContentType = stagedPackContentType
	if _, err := w.Write(data); err != nil {
		// Abandon the partial upload; otherwise the failed object can linger.
		_ = w.Close()
		return fmt.Errorf("write %s: %w", uri, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("commit %s: %w", uri, err)
	}
	return nil
}
