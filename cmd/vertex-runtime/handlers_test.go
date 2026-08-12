package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnaryHandler_Success(t *testing.T) {
	turn := func(_ context.Context, method string, input map[string]any) (any, error) {
		if method != "query" {
			t.Errorf("method = %q, want \"query\"", method)
		}
		return "hello " + input["message"].(string), nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/reasoning_engine",
		strings.NewReader(`{"class_method":"query","input":{"message":"world"}}`))

	newUnaryHandler(turn).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got contractResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.Output != "hello world" {
		t.Errorf("output = %v", got.Output)
	}
}

func TestUnaryHandler_RejectsGET(t *testing.T) {
	turn := func(context.Context, string, map[string]any) (any, error) { return nil, nil }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/reasoning_engine", nil)

	newUnaryHandler(turn).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestUnaryHandler_MalformedJSON(t *testing.T) {
	turn := func(context.Context, string, map[string]any) (any, error) { return nil, nil }

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/reasoning_engine",
		strings.NewReader(`{not json`))

	newUnaryHandler(turn).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestUnaryHandler_TurnError(t *testing.T) {
	turn := func(context.Context, string, map[string]any) (any, error) {
		return nil, fmt.Errorf("model unavailable")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/reasoning_engine",
		strings.NewReader(`{"class_method":"query","input":{}}`))

	newUnaryHandler(turn).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "model unavailable") {
		t.Errorf("body should carry the error, got %s", rec.Body.String())
	}
}

func TestUnaryHandler_DefaultsClassMethod(t *testing.T) {
	var seen string
	turn := func(_ context.Context, method string, _ map[string]any) (any, error) {
		seen = method
		return "ok", nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/reasoning_engine",
		strings.NewReader(`{"input":{}}`))

	newUnaryHandler(turn).ServeHTTP(rec, req)

	if seen != classMethodQuery {
		t.Errorf("method = %q, want %q", seen, classMethodQuery)
	}
}

// staticStream returns a streamFunc emitting the given texts then closing.
func staticStream(t *testing.T, wantMethod string, texts ...string) streamFunc {
	t.Helper()
	return func(_ context.Context, method string, _ map[string]any) (<-chan string, <-chan error) {
		if wantMethod != "" && method != wantMethod {
			t.Errorf("method = %q, want %q", method, wantMethod)
		}
		out := make(chan string, len(texts))
		errCh := make(chan error, 1)
		for _, text := range texts {
			out <- text
		}
		close(out)
		close(errCh)
		return out, errCh
	}
}

func TestStreamHandler_EmitsOneLinePerChunk(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stream_reasoning_engine",
		strings.NewReader(`{"class_method":"stream_query","input":{}}`))

	newStreamHandler(staticStream(t, classMethodStreamQuery, "Hello", " ", "world")).
		ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != contentTypeNDJSON {
		t.Errorf("content-type = %q", ct)
	}

	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 ndjson lines, got %d: %q", len(lines), rec.Body.String())
	}

	want := []string{"Hello", " ", "world"}
	for i, line := range lines {
		var got contractResponse
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("unmarshal line %d: %v", i, err)
		}
		if got.Output != want[i] {
			t.Errorf("line %d output = %v, want %q", i, got.Output, want[i])
		}
	}
}

func TestStreamHandler_RejectsGET(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/stream_reasoning_engine", nil)

	newStreamHandler(staticStream(t, "")).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestStreamHandler_ErrorBeforeAnyChunk(t *testing.T) {
	stream := func(context.Context, string, map[string]any) (<-chan string, <-chan error) {
		out := make(chan string)
		close(out)
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("model unavailable")
		close(errCh)
		return out, errCh
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stream_reasoning_engine",
		strings.NewReader(`{"input":{}}`))

	newStreamHandler(stream).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestStreamHandler_ErrorAfterChunksIsTrailingLine(t *testing.T) {
	stream := func(context.Context, string, map[string]any) (<-chan string, <-chan error) {
		out := make(chan string, 1)
		out <- "partial"
		close(out)
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("mid-stream failure")
		close(errCh)
		return out, errCh
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/stream_reasoning_engine",
		strings.NewReader(`{"input":{}}`))

	newStreamHandler(stream).ServeHTTP(rec, req)

	// Headers are already sent, so the status stays 200 and the error is
	// reported as a trailing ndjson line.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), rec.Body.String())
	}
	if !strings.Contains(lines[1], "mid-stream failure") {
		t.Errorf("final line should carry the error, got %q", lines[1])
	}
}
