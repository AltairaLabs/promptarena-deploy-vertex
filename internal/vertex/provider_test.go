package vertex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/promptarena/deploy"
)

func TestProviderSatisfiesInterface(t *testing.T) {
	var _ deploy.Provider = NewProvider()
}

func TestGetProviderInfo(t *testing.T) {
	info, err := NewProvider().GetProviderInfo(context.Background())
	if err != nil {
		t.Fatalf("GetProviderInfo: %v", err)
	}
	if info.Name != ProviderName {
		t.Errorf("Name = %q, want %q", info.Name, ProviderName)
	}
	if info.Version == "" {
		t.Error("Version is empty")
	}
	if info.ConfigSchema == "" {
		t.Fatal("ConfigSchema is empty")
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(info.ConfigSchema), &schema); err != nil {
		t.Fatalf("ConfigSchema is not valid JSON: %v", err)
	}
}

func TestUnimplementedMethodsReturnErrors(t *testing.T) {
	p := NewProvider()
	ctx := context.Background()

	if _, err := p.Plan(ctx, &deploy.PlanRequest{}); err == nil {
		t.Error("Plan should return an error in this phase")
	}
	if _, err := p.Apply(ctx, &deploy.PlanRequest{}, nil); err == nil {
		t.Error("Apply should return an error in this phase")
	}
	if err := p.Destroy(ctx, &deploy.DestroyRequest{}, nil); err == nil {
		t.Error("Destroy should return an error in this phase")
	}
	if _, err := p.Status(ctx, &deploy.StatusRequest{}); err == nil {
		t.Error("Status should return an error in this phase")
	}
	if _, err := p.Import(ctx, &deploy.ImportRequest{}); err == nil {
		t.Error("Import should return an error in this phase")
	}
}
