package main

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestResolvePackFile_Inline(t *testing.T) {
	cfg := &runtimeConfig{PackJSON: `{"id":"demo"}`}
	dir := t.TempDir()

	path, err := resolvePackFile(context.Background(), cfg, dir, nil)
	if err != nil {
		t.Fatalf("resolvePackFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written pack: %v", err)
	}
	if string(data) != `{"id":"demo"}` {
		t.Errorf("contents = %q", string(data))
	}
}

func TestResolvePackFile_URI(t *testing.T) {
	cfg := &runtimeConfig{PackURI: "gs://bucket/pack.json"}
	dir := t.TempDir()

	fetch := func(_ context.Context, uri string) ([]byte, error) {
		if uri != "gs://bucket/pack.json" {
			t.Errorf("uri = %q", uri)
		}
		return []byte(`{"id":"remote"}`), nil
	}

	path, err := resolvePackFile(context.Background(), cfg, dir, fetch)
	if err != nil {
		t.Fatalf("resolvePackFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written pack: %v", err)
	}
	if string(data) != `{"id":"remote"}` {
		t.Errorf("contents = %q", string(data))
	}
}

func TestResolvePackFile_InlineWins(t *testing.T) {
	cfg := &runtimeConfig{PackJSON: `{"id":"inline"}`, PackURI: "gs://bucket/pack.json"}
	dir := t.TempDir()

	fetch := func(context.Context, string) ([]byte, error) {
		t.Fatal("fetcher must not be called when inline pack is present")
		return nil, nil
	}

	path, err := resolvePackFile(context.Background(), cfg, dir, fetch)
	if err != nil {
		t.Fatalf("resolvePackFile: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != `{"id":"inline"}` {
		t.Errorf("contents = %q", string(data))
	}
}

func TestResolvePackFile_FetchError(t *testing.T) {
	cfg := &runtimeConfig{PackURI: "gs://bucket/pack.json"}
	dir := t.TempDir()

	fetch := func(context.Context, string) ([]byte, error) {
		return nil, fmt.Errorf("boom")
	}

	if _, err := resolvePackFile(context.Background(), cfg, dir, fetch); err == nil {
		t.Fatal("expected error from failing fetcher, got nil")
	}
}

func TestParseGCSURI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantObject string
		wantErr    bool
	}{
		{"valid", "gs://my-bucket/path/pack.json", "my-bucket", "path/pack.json", false},
		{"wrong scheme", "s3://my-bucket/pack.json", "", "", true},
		{"no object", "gs://my-bucket", "", "", true},
		{"empty object", "gs://my-bucket/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, object, err := parseGCSURI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseGCSURI: %v", err)
			}
			if bucket != tt.wantBucket || object != tt.wantObject {
				t.Errorf("got (%q, %q), want (%q, %q)",
					bucket, object, tt.wantBucket, tt.wantObject)
			}
		})
	}
}
