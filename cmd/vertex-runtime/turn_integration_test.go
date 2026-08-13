package main

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// testPackFile is a minimal single-prompt pack vendored into testdata so these
// tests do not depend on a sibling PromptKit checkout.
const testPackFile = "testdata/test.pack.json"

// testAgentName is the only prompt in testPackFile.
const testAgentName = "chat"

// mockOpts returns SDK options backed by the in-memory mock provider, so turn
// execution is exercised end to end with no network and no credentials.
func mockOpts() []sdk.Option {
	return []sdk.Option{
		sdk.WithProvider(mock.NewProvider("mock", "mock-model", false)),
	}
}

func TestNewTurnFunc_ReturnsText(t *testing.T) {
	turn := newTurnFunc(testPackFile, testAgentName, mockOpts(), nil)

	got, err := turn(context.Background(), classMethodQuery,
		map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}

	text, ok := got.(string)
	if !ok {
		t.Fatalf("output is %T, want string", got)
	}
	if text == "" {
		t.Error("expected non-empty text from the mock provider")
	}
}

func TestNewTurnFunc_MissingMessage(t *testing.T) {
	turn := newTurnFunc(testPackFile, testAgentName, mockOpts(), nil)

	if _, err := turn(context.Background(), classMethodQuery, map[string]any{}); err == nil {
		t.Fatal("expected an error when no message field is present")
	}
}

func TestNewTurnFunc_BadPackPath(t *testing.T) {
	turn := newTurnFunc("testdata/does-not-exist.pack.json", testAgentName, mockOpts(), nil)

	_, err := turn(context.Background(), classMethodQuery,
		map[string]any{"message": "hello"})
	if err == nil {
		t.Fatal("expected an error for a missing pack file")
	}
	if !strings.Contains(err.Error(), "open conversation") {
		t.Errorf("error should be wrapped with context, got %v", err)
	}
}

func TestNewStreamFunc_StreamsText(t *testing.T) {
	stream := newStreamFunc(testPackFile, testAgentName, mockOpts(), nil)

	chunks, errs := stream(context.Background(), classMethodStreamQuery,
		map[string]any{"message": "hello"})

	var got []string
	for text := range chunks {
		got = append(got, text)
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream: %v", err)
	}

	if len(got) == 0 {
		t.Fatal("expected at least one text chunk")
	}
	if strings.Join(got, "") == "" {
		t.Error("chunks joined to an empty string")
	}
}

func TestNewStreamFunc_MissingMessage(t *testing.T) {
	stream := newStreamFunc(testPackFile, testAgentName, mockOpts(), nil)

	chunks, errs := stream(context.Background(), classMethodStreamQuery, map[string]any{})

	for range chunks { //nolint:revive // draining the channel is the point
	}
	if err := <-errs; err == nil {
		t.Fatal("expected an error when no message field is present")
	}
}

func TestNewStreamFunc_BadPackPath(t *testing.T) {
	stream := newStreamFunc("testdata/does-not-exist.pack.json", testAgentName, mockOpts(), nil)

	chunks, errs := stream(context.Background(), classMethodStreamQuery,
		map[string]any{"message": "hello"})

	for range chunks { //nolint:revive // draining the channel is the point
	}
	err := <-errs
	if err == nil {
		t.Fatal("expected an error for a missing pack file")
	}
	if !strings.Contains(err.Error(), "open conversation") {
		t.Errorf("error should be wrapped with context, got %v", err)
	}
}

func TestNewStreamFunc_CanceledContext(t *testing.T) {
	stream := newStreamFunc(testPackFile, testAgentName, mockOpts(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	chunks, errs := stream(ctx, classMethodStreamQuery, map[string]any{"message": "hello"})

	for range chunks { //nolint:revive // draining the channel is the point
	}
	// A canceled context must terminate the stream rather than hang. Whether an
	// error surfaces depends on how far the turn got before cancellation landed,
	// so this asserts termination, not a specific error.
	<-errs
}
