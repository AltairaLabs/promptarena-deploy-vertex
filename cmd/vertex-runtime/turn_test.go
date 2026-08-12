package main

import "testing"

func TestExtractMessage_MessageKey(t *testing.T) {
	got, err := extractMessage(map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("extractMessage: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want \"hello\"", got)
	}
}

func TestExtractMessage_InputKey(t *testing.T) {
	got, err := extractMessage(map[string]any{"input": "hi there"})
	if err != nil {
		t.Fatalf("extractMessage: %v", err)
	}
	if got != "hi there" {
		t.Errorf("got %q, want \"hi there\"", got)
	}
}

func TestExtractMessage_PromptKey(t *testing.T) {
	got, err := extractMessage(map[string]any{"prompt": "explain"})
	if err != nil {
		t.Fatalf("extractMessage: %v", err)
	}
	if got != "explain" {
		t.Errorf("got %q, want \"explain\"", got)
	}
}

func TestExtractMessage_MessageWins(t *testing.T) {
	got, err := extractMessage(map[string]any{"message": "a", "input": "b", "prompt": "c"})
	if err != nil {
		t.Fatalf("extractMessage: %v", err)
	}
	if got != "a" {
		t.Errorf("got %q, want \"a\"", got)
	}
}

func TestExtractMessage_Missing(t *testing.T) {
	if _, err := extractMessage(map[string]any{"other": "x"}); err == nil {
		t.Fatal("expected error when no message key is present, got nil")
	}
}

func TestExtractMessage_NonString(t *testing.T) {
	if _, err := extractMessage(map[string]any{"message": 42}); err == nil {
		t.Fatal("expected error for non-string message, got nil")
	}
}
