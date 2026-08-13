package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

// discardLogger keeps server test output quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testMux builds a mux whose handlers are never actually invoked by these
// tests; runServer's lifecycle is what is under test.
func testMux(t *testing.T) *http.ServeMux {
	t.Helper()
	turn := func(context.Context, string, map[string]any) (any, error) { return "ok", nil }
	return buildMux(turn, staticStream(t, "", "ok"))
}

func TestRunServer_ShutsDownOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runServer(ctx, discardLogger(), "127.0.0.1:0", testMux(t))
	}()

	// Give the listener a moment to bind before asking it to stop, so the
	// shutdown path is what runs rather than a cancelled Listen.
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServer returned %v, want nil on clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runServer did not return within 10s of context cancellation")
	}
}

func TestRunServer_ListenError(t *testing.T) {
	err := runServer(context.Background(), discardLogger(), "127.0.0.1:99999", testMux(t))
	if err == nil {
		t.Fatal("expected an error for an out-of-range port")
	}
}
