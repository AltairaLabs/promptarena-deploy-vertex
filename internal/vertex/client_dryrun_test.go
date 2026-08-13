package vertex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDryRunClient_SatisfiesInterface(t *testing.T) {
	var _ gcpClient = newDryRunClient(&Config{})
}

func TestDryRunClient_CreateThenGet(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})

	engine, err := client.CreateEngine(context.Background(), &EngineSpec{
		DisplayName: "assistant",
		ImageURI:    "us-central1-docker.pkg.dev/p/r/i",
		Labels:      map[string]string{LabelManagedBy: ManagedByValue},
	})
	if err != nil {
		t.Fatalf("CreateEngine: %v", err)
	}
	if !strings.Contains(engine.ResourceName, "/reasoningEngines/") {
		t.Errorf("ResourceName = %q, want a full resource name", engine.ResourceName)
	}
	if engine.State != EngineStateActive {
		t.Errorf("State = %q, want %q", engine.State, EngineStateActive)
	}

	got, err := client.GetEngine(context.Background(), engine.ResourceName)
	if err != nil {
		t.Fatalf("GetEngine: %v", err)
	}
	if got.DisplayName != "assistant" {
		t.Errorf("DisplayName = %q", got.DisplayName)
	}
}

func TestDryRunClient_CreateIsDeterministic(t *testing.T) {
	a := newDryRunClient(&Config{Project: "p", Location: "l"})
	b := newDryRunClient(&Config{Project: "p", Location: "l"})

	first, _ := a.CreateEngine(context.Background(), &EngineSpec{DisplayName: "x"})
	second, _ := b.CreateEngine(context.Background(), &EngineSpec{DisplayName: "x"})

	if first.ResourceName != second.ResourceName {
		t.Errorf("dry-run names must be deterministic: %q vs %q",
			first.ResourceName, second.ResourceName)
	}
}

func TestDryRunClient_GetEngineNotFound(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "us-central1"})

	_, err := client.GetEngine(context.Background(),
		"projects/p/locations/us-central1/reasoningEngines/123")
	if !errors.Is(err, ErrEngineNotFound) {
		t.Errorf("err = %v, want ErrEngineNotFound", err)
	}
}

func TestDryRunClient_UpdateEngine(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})
	created, _ := client.CreateEngine(context.Background(), &EngineSpec{DisplayName: "x"})

	updated, err := client.UpdateEngine(context.Background(), created.ResourceName,
		&EngineSpec{DisplayName: "renamed"})
	if err != nil {
		t.Fatalf("UpdateEngine: %v", err)
	}
	if updated.DisplayName != "renamed" {
		t.Errorf("DisplayName = %q", updated.DisplayName)
	}
}

func TestDryRunClient_UpdateMissingEngine(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})

	_, err := client.UpdateEngine(context.Background(),
		"projects/p/locations/l/reasoningEngines/nope", &EngineSpec{DisplayName: "x"})
	if !errors.Is(err, ErrEngineNotFound) {
		t.Errorf("err = %v, want ErrEngineNotFound", err)
	}
}

func TestDryRunClient_DeleteEngine(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})
	created, _ := client.CreateEngine(context.Background(), &EngineSpec{DisplayName: "x"})

	if err := client.DeleteEngine(context.Background(), created.ResourceName); err != nil {
		t.Fatalf("DeleteEngine: %v", err)
	}
	if _, err := client.GetEngine(context.Background(), created.ResourceName); !errors.Is(err, ErrEngineNotFound) {
		t.Error("engine should be gone after delete")
	}
}

func TestDryRunClient_DeleteMissingEngineIsFine(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})

	if err := client.DeleteEngine(context.Background(),
		"projects/p/locations/l/reasoningEngines/nope"); err != nil {
		t.Errorf("deleting a missing engine should be idempotent, got %v", err)
	}
}

func TestDryRunClient_ListEnginesByLabel(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})
	if _, err := client.CreateEngine(context.Background(), &EngineSpec{
		DisplayName: "managed",
		Labels:      map[string]string{LabelManagedBy: ManagedByValue},
	}); err != nil {
		t.Fatalf("CreateEngine: %v", err)
	}
	if _, err := client.CreateEngine(context.Background(), &EngineSpec{
		DisplayName: "foreign",
		Labels:      map[string]string{LabelManagedBy: "someone-else"},
	}); err != nil {
		t.Fatalf("CreateEngine: %v", err)
	}

	got, err := client.ListEnginesByLabel(context.Background(), "p", "l",
		map[string]string{LabelManagedBy: ManagedByValue})
	if err != nil {
		t.Fatalf("ListEnginesByLabel: %v", err)
	}
	if len(got) != 1 || got[0].DisplayName != "managed" {
		t.Errorf("got %+v, want only the managed engine", got)
	}
}

func TestDryRunClient_ListWrongProjectIsEmpty(t *testing.T) {
	client := newDryRunClient(&Config{Project: "p", Location: "l"})
	if _, err := client.CreateEngine(context.Background(),
		&EngineSpec{DisplayName: "x"}); err != nil {
		t.Fatalf("CreateEngine: %v", err)
	}

	got, err := client.ListEnginesByLabel(context.Background(), "other", "l", nil)
	if err != nil {
		t.Fatalf("ListEnginesByLabel: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("a different project should match nothing, got %+v", got)
	}
}
