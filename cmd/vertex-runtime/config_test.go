package main

import "testing"

func TestLoadConfig_RequiresPackSource(t *testing.T) {
	t.Setenv(envPackJSON, "")
	t.Setenv(envPackURI, "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error when neither pack source is set, got nil")
	}
}

func TestLoadConfig_InlinePack(t *testing.T) {
	t.Setenv(envPackJSON, `{"id":"demo"}`)
	t.Setenv(envAgentName, "assistant")
	t.Setenv(envProject, "my-project")
	t.Setenv(envLocation, "us-central1")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.PackJSON != `{"id":"demo"}` {
		t.Errorf("PackJSON = %q", cfg.PackJSON)
	}
	if cfg.AgentName != "assistant" {
		t.Errorf("AgentName = %q", cfg.AgentName)
	}
	if cfg.Project != "my-project" {
		t.Errorf("Project = %q", cfg.Project)
	}
	if cfg.Location != "us-central1" {
		t.Errorf("Location = %q", cfg.Location)
	}
	if cfg.Port != contractPort {
		t.Errorf("Port = %d, want %d", cfg.Port, contractPort)
	}
}

func TestLoadConfig_PackURIAlone(t *testing.T) {
	t.Setenv(envPackJSON, "")
	t.Setenv(envPackURI, "gs://bucket/pack.json")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.PackURI != "gs://bucket/pack.json" {
		t.Errorf("PackURI = %q", cfg.PackURI)
	}
}
