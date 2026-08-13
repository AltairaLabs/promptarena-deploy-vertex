package vertex

import (
	"testing"

	aiplatformpb "cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
)

func TestSpecToProto_SetsContainerImageThroughOneof(t *testing.T) {
	got := specToProto(&EngineSpec{
		DisplayName: "assistant",
		ImageURI:    "us-central1-docker.pkg.dev/p/r/i:v1",
	})

	if got.GetDisplayName() != "assistant" {
		t.Errorf("DisplayName = %q", got.GetDisplayName())
	}
	if got.GetSpec().GetContainerSpec().GetImageUri() != "us-central1-docker.pkg.dev/p/r/i:v1" {
		t.Errorf("ImageUri = %q", got.GetSpec().GetContainerSpec().GetImageUri())
	}
	if got.GetSpec().GetPackageSpec() != nil {
		t.Error("container deployment must not also set a package spec")
	}
}

func TestSpecToProto_EnvIsSortedForDeterminism(t *testing.T) {
	got := specToProto(&EngineSpec{
		DisplayName: "a",
		Env:         map[string]string{"B_VAR": "2", "A_VAR": "1"},
	})

	env := got.GetSpec().GetDeploymentSpec().GetEnv()
	if len(env) != 2 {
		t.Fatalf("len(env) = %d, want 2", len(env))
	}
	if env[0].GetName() != "A_VAR" || env[1].GetName() != "B_VAR" {
		t.Errorf("env is not sorted by name: %v", env)
	}
	if env[0].GetValue() != "1" {
		t.Errorf("A_VAR = %q", env[0].GetValue())
	}
}

func TestSpecToProto_OmitsEmptyDeploymentSpec(t *testing.T) {
	got := specToProto(&EngineSpec{DisplayName: "a"})

	if got.GetSpec().GetDeploymentSpec() != nil {
		t.Error("an engine with no env or limits should not carry a deployment spec")
	}
}

func TestSpecToProto_ServiceAccountAndLabels(t *testing.T) {
	got := specToProto(&EngineSpec{
		DisplayName:    "a",
		ServiceAccount: "sa@p.iam.gserviceaccount.com",
		Labels:         map[string]string{"team": "platform"},
	})

	if got.GetSpec().GetServiceAccount() != "sa@p.iam.gserviceaccount.com" {
		t.Errorf("ServiceAccount = %q", got.GetSpec().GetServiceAccount())
	}
	if got.GetLabels()["team"] != "platform" {
		t.Errorf("Labels = %v", got.GetLabels())
	}
}

func TestSpecToProto_EmptyServiceAccountStaysUnset(t *testing.T) {
	got := specToProto(&EngineSpec{DisplayName: "a"})

	if got.GetSpec().GetServiceAccount() != "" {
		t.Errorf("ServiceAccount = %q, want unset so the API default applies",
			got.GetSpec().GetServiceAccount())
	}
}

func TestSpecToProto_ResourceLimitsAndInstances(t *testing.T) {
	minI, maxI := 0, 3
	got := specToProto(&EngineSpec{
		DisplayName:    "a",
		ResourceLimits: map[string]string{"cpu": "2", "memory": "4Gi"},
		MinInstances:   &minI,
		MaxInstances:   &maxI,
	})

	deployment := got.GetSpec().GetDeploymentSpec()
	if deployment.GetResourceLimits()["cpu"] != "2" {
		t.Errorf("ResourceLimits = %v", deployment.GetResourceLimits())
	}
	if deployment.GetMinInstances() != 0 {
		t.Errorf("MinInstances = %d", deployment.GetMinInstances())
	}
	if deployment.GetMaxInstances() != 3 {
		t.Errorf("MaxInstances = %d", deployment.GetMaxInstances())
	}
}

func TestEngineFromProto(t *testing.T) {
	got := engineFromProto(&aiplatformpb.ReasoningEngine{
		Name:        "projects/p/locations/l/reasoningEngines/1",
		DisplayName: "assistant",
		Labels:      map[string]string{"k": "v"},
	})

	if got.ResourceName != "projects/p/locations/l/reasoningEngines/1" {
		t.Errorf("ResourceName = %q", got.ResourceName)
	}
	if got.DisplayName != "assistant" {
		t.Errorf("DisplayName = %q", got.DisplayName)
	}
	if got.Labels["k"] != "v" {
		t.Errorf("Labels = %v", got.Labels)
	}
	if got.State != EngineStateActive {
		t.Errorf("State = %q; a returned engine is serving", got.State)
	}
}
