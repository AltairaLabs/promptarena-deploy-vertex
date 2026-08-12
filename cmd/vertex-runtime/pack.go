package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloud.google.com/go/storage"
)

// packFileName is the local filename the resolved pack is written to.
const packFileName = "pack.json"

// packFilePerm is the permission mode for the written pack file.
const packFilePerm = 0o600

// gcsFetcher retrieves the bytes at a gs:// URI. Injected so tests do not
// require a GCS client.
type gcsFetcher func(ctx context.Context, uri string) ([]byte, error)

// resolvePackFile materialises the pack as a local file in dir and returns its
// path. An inline pack takes precedence over a URI; the fetcher is only
// consulted when the pack must be retrieved from GCS.
func resolvePackFile(
	ctx context.Context, cfg *runtimeConfig, dir string, fetch gcsFetcher,
) (string, error) {
	data := []byte(cfg.PackJSON)

	if len(data) == 0 {
		if fetch == nil {
			return "", fmt.Errorf("no inline pack and no fetcher configured")
		}
		fetched, err := fetch(ctx, cfg.PackURI)
		if err != nil {
			return "", fmt.Errorf("fetch pack from %s: %w", cfg.PackURI, err)
		}
		data = fetched
	}

	path := filepath.Join(dir, packFileName)
	if err := os.WriteFile(path, data, packFilePerm); err != nil {
		return "", fmt.Errorf("write pack file: %w", err)
	}
	return path, nil
}

// fetchGCS reads an object addressed by a gs://bucket/object URI using
// Application Default Credentials.
func fetchGCS(ctx context.Context, uri string) ([]byte, error) {
	bucket, object, err := parseGCSURI(uri)
	if err != nil {
		return nil, err
	}

	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage client: %w", err)
	}
	defer func() { _ = client.Close() }()

	reader, err := client.Bucket(bucket).Object(object).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("open gs://%s/%s: %w", bucket, object, err)
	}
	defer func() { _ = reader.Close() }()

	return io.ReadAll(reader)
}

// parseGCSURI splits a gs://bucket/object URI into its bucket and object parts.
func parseGCSURI(uri string) (bucket, object string, err error) {
	const scheme = "gs://"
	if !strings.HasPrefix(uri, scheme) {
		return "", "", fmt.Errorf("pack URI %q must start with %s", uri, scheme)
	}
	rest := strings.TrimPrefix(uri, scheme)
	bucket, object, found := strings.Cut(rest, "/")
	if !found || bucket == "" || object == "" {
		return "", "", fmt.Errorf("pack URI %q must be gs://bucket/object", uri)
	}
	return bucket, object, nil
}
