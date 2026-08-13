package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolSpecs_Empty(t *testing.T) {
	got, err := parseToolSpecs("")
	if err != nil {
		t.Fatalf("parseToolSpecs: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestParseToolSpecs_Invalid(t *testing.T) {
	if _, err := parseToolSpecs(`{not json`); err == nil {
		t.Fatal("expected an error for malformed tool specs")
	}
}

func TestParseToolSpecs_MockAndHTTP(t *testing.T) {
	raw := `{
		"lookup_order": {"name":"lookup_order","mode":"mock","mock_result":"Order 42: shipped"},
		"get_weather": {"name":"get_weather","mode":"live",
		                "http":{"url":"https://api.example.com/weather","method":"POST"}}
	}`

	got, err := parseToolSpecs(raw)
	if err != nil {
		t.Fatalf("parseToolSpecs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got["lookup_order"].Mode != "mock" {
		t.Errorf("mode = %q", got["lookup_order"].Mode)
	}
	if got["get_weather"].HTTP == nil ||
		got["get_weather"].HTTP.URL != "https://api.example.com/weather" {
		t.Errorf("http = %+v", got["get_weather"].HTTP)
	}
}

func TestMockHandler_StaticResult(t *testing.T) {
	handler := mockHandler(toolSpec{Name: "lookup_order", MockResult: "Order 42: shipped"})

	got, err := handler(map[string]any{"order_id": "42"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if got != "Order 42: shipped" {
		t.Errorf("got %v", got)
	}
}

func TestMockHandler_TemplateRendersArgs(t *testing.T) {
	handler := mockHandler(toolSpec{
		Name:         "lookup_order",
		MockTemplate: `{"order":"{{.order_id}}","status":"shipped"}`,
	})

	got, err := handler(map[string]any{"order_id": "42"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	rendered, ok := got.(string)
	if !ok {
		t.Fatalf("got %T, want string", got)
	}
	if !strings.Contains(rendered, `"order":"42"`) {
		t.Errorf("template did not interpolate args: %s", rendered)
	}
}

func TestMockHandler_BadTemplateErrors(t *testing.T) {
	handler := mockHandler(toolSpec{Name: "x", MockTemplate: `{{.unclosed`})

	if _, err := handler(map[string]any{}); err == nil {
		t.Fatal("a malformed template should error rather than return the raw text")
	}
}

func TestMockHandler_NoMockConfigured(t *testing.T) {
	handler := mockHandler(toolSpec{Name: "x"})

	if _, err := handler(map[string]any{}); err == nil {
		t.Fatal("a mock tool with neither mock_result nor mock_template should error")
	}
}

func TestParseToolSpecs_RoundTripsMockResultObject(t *testing.T) {
	raw := `{"t":{"name":"t","mode":"mock","mock_result":{"status":"ok","count":3}}}`

	got, err := parseToolSpecs(raw)
	if err != nil {
		t.Fatalf("parseToolSpecs: %v", err)
	}

	encoded, err := json.Marshal(got["t"].MockResult)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"status":"ok"`) {
		t.Errorf("object mock_result lost in decode: %s", encoded)
	}
}
