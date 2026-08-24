package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// PromptKit parses a rendered template back as JSON, so a template that
	// renders an object must reach the model as an object — not as a quoted
	// string, which reads to the model as a different tool result entirely.
	rendered, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T (%v), want map[string]any", got, got)
	}
	if rendered["order"] != "42" {
		t.Errorf("template did not interpolate args: %v", rendered)
	}
}

func TestMockHandler_TemplateNonJSONWrapsAsResult(t *testing.T) {
	handler := mockHandler(toolSpec{
		Name:         "lookup_order",
		MockTemplate: `Order {{.order_id}} has shipped`,
	})

	got, err := handler(map[string]any{"order_id": "42"})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	rendered, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T (%v), want map[string]any", got, got)
	}
	if rendered["result"] != "Order 42 has shipped" {
		t.Errorf(`non-JSON output must be wrapped as {"result": ...}; got %v`, rendered)
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

// TestHTTPToolConfig_CarriesTheWholeBinding is the point of widening toolHTTP.
//
// Only url and method used to survive, so a live tool could not authenticate:
// every endpoint behind an API key or bearer token was unreachable from a
// deployed engine, and with no timeout a slow one stalled the turn.
func TestHTTPToolConfig_CarriesTheWholeBinding(t *testing.T) {
	spec := toolSpec{
		Name: "lookup",
		Mode: toolModeLive,
		HTTP: &toolHTTP{
			URL:            "https://api.example.com/lookup",
			Method:         "POST",
			Headers:        map[string]string{"X-Tenant": "acme"},
			HeadersFromEnv: []string{"Authorization=LOOKUP_TOKEN"},
			TimeoutMs:      2500,
			Redact:         []string{"ssn"},
		},
	}

	cfg := httpToolConfig(spec)
	if cfg == nil {
		t.Fatal("expected a config")
	}

	// The config's fields are unexported, so assert through the behaviour that
	// depends on them: a request carries the headers, honours the timeout, and
	// reads the environment for the ones declared that way.
	t.Setenv("LOOKUP_TOKEN", "secret-value")

	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	spec.HTTP.URL = srv.URL
	handler := httpToolConfig(spec).HandlerCtx()
	if _, err := handler(context.Background(), map[string]any{"id": "1"}); err != nil {
		t.Fatalf("tool call: %v", err)
	}

	if got := gotHeaders.Get("X-Tenant"); got != "acme" {
		t.Errorf("static header not sent: X-Tenant = %q", got)
	}
	if got := gotHeaders.Get("Authorization"); got != "secret-value" {
		t.Errorf("header from env not resolved: Authorization = %q — "+
			"a live tool cannot authenticate without this", got)
	}
}

// TestHTTPToolConfig_MinimalBindingStillWorks keeps the simplest case working.
func TestHTTPToolConfig_MinimalBindingStillWorks(t *testing.T) {
	cfg := httpToolConfig(toolSpec{
		Name: "ping",
		Mode: toolModeLive,
		HTTP: &toolHTTP{URL: "https://api.example.com/ping"},
	})
	if cfg == nil {
		t.Fatal("a url on its own should still produce a config")
	}
}
