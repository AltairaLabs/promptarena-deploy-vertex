package vertex

import "testing"

func TestHashString_IsStable(t *testing.T) {
	if hashString("abc") != hashString("abc") {
		t.Error("hashString is not deterministic")
	}
	if hashString("abc") == hashString("abd") {
		t.Error("different inputs produced the same hash")
	}
}

func TestHashString_Empty(t *testing.T) {
	if hashString("") == "" {
		t.Error("expected a hash for the empty string")
	}
}

func TestHashPack_DistinguishesPacks(t *testing.T) {
	if hashPack(`{"id":"a"}`) == hashPack(`{"id":"b"}`) {
		t.Error("different packs produced the same hash")
	}
}

func TestHashPlanConfig_StableAcrossCalls(t *testing.T) {
	cfg := &Config{Project: "p", Location: "us-central1", Image: "img"}
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "gemini", Model: "m"}}

	first, err := hashPlanConfig(cfg, resolved, "")
	if err != nil {
		t.Fatalf("hashPlanConfig: %v", err)
	}
	second, err := hashPlanConfig(cfg, resolved, "")
	if err != nil {
		t.Fatalf("hashPlanConfig: %v", err)
	}
	if first != second {
		t.Error("hashPlanConfig is not deterministic")
	}
}

func TestHashPlanConfig_IgnoresLabelOrder(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	a := &Config{Project: "p", Location: "l", Labels: map[string]string{"x": "1", "y": "2"}}
	b := &Config{Project: "p", Location: "l", Labels: map[string]string{"y": "2", "x": "1"}}

	hashA, err := hashPlanConfig(a, resolved, "")
	if err != nil {
		t.Fatalf("hashPlanConfig: %v", err)
	}
	hashB, err := hashPlanConfig(b, resolved, "")
	if err != nil {
		t.Fatalf("hashPlanConfig: %v", err)
	}
	if hashA != hashB {
		t.Error("label map ordering changed the config hash")
	}
}

func TestHashPlanConfig_ChangesWithServiceAccount(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	a := &Config{Project: "p", Location: "l"}
	b := &Config{Project: "p", Location: "l", ServiceAccount: "sa@p.iam.gserviceaccount.com"}

	hashA, _ := hashPlanConfig(a, resolved, "")
	hashB, _ := hashPlanConfig(b, resolved, "")
	if hashA == hashB {
		t.Error("service_account change should change the config hash")
	}
}

func TestHashPlanConfig_ChangesWithBindings(t *testing.T) {
	cfg := &Config{Project: "p", Location: "l"}
	a := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "gemini", Model: "m1"}}
	b := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "gemini", Model: "m2"}}

	hashA, _ := hashPlanConfig(cfg, a, "")
	hashB, _ := hashPlanConfig(cfg, b, "")
	if hashA == hashB {
		t.Error("a model change should change the config hash")
	}
}

func TestHashPlanConfig_ChangesWithToolSpecs(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	cfg := &Config{Project: "p", Location: "l"}

	hashA, _ := hashPlanConfig(cfg, resolved, `{"t":{"mode":"mock","mock_result":"a"}}`)
	hashB, _ := hashPlanConfig(cfg, resolved, `{"t":{"mode":"mock","mock_result":"b"}}`)
	if hashA == hashB {
		t.Error("a mock_result change should change the config hash; " +
			"tool specs are deployed as container env")
	}
}

func TestHashPlanConfig_ChangesWithObservability(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	a := &Config{Project: "p", Location: "l"}
	b := &Config{Project: "p", Location: "l", Observability: &Observability{
		TracingEnabled: true,
		OTLPEndpoint:   "http://collector:4317",
	}}

	hashA, _ := hashPlanConfig(a, resolved, "")
	hashB, _ := hashPlanConfig(b, resolved, "")
	if hashA == hashB {
		t.Error("enabling tracing should change the config hash")
	}
}

func TestHashPlanConfig_IgnoresDryRun(t *testing.T) {
	resolved := []ResolvedBinding{{Name: "default", Role: RoleLLM, Type: "g", Model: "m"}}
	a := &Config{Project: "p", Location: "l"}
	b := &Config{Project: "p", Location: "l", DryRun: true}

	hashA, _ := hashPlanConfig(a, resolved, "")
	hashB, _ := hashPlanConfig(b, resolved, "")
	if hashA != hashB {
		t.Error("dry_run is not deployed state and must not affect the config hash")
	}
}
