package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// warnOnce ensures unsupported tool modes are reported once per process rather
// than on every request.
var warnOnce sync.Once

// warnUnsupportedTools logs tool modes this runtime cannot execute. A tool that
// looks configured but never runs is exactly the failure this path prevents, so
// it must be visible.
func warnUnsupportedTools(unsupported []string) {
	if len(unsupported) == 0 {
		return
	}
	warnOnce.Do(func() {
		slog.Warn("tools declared with unsupported execution modes will not run",
			"tools", strings.Join(unsupported, ", "))
	})
}

// messageKeys are the accepted input field names for the user turn, in
// precedence order. "prompt" and "input" match common hyperscaler conventions;
// "message" is the canonical PromptKit name.
var messageKeys = []string{"message", "input", "prompt"}

// sessionKeys are the accepted input field names for the conversation to
// continue, in precedence order.
var sessionKeys = []string{"session_id", "sessionId", "session"}

// extractSession pulls the session id out of the contract request input.
//
// Absent means a one-off turn, which is what every request was before sessions
// existed — so a caller that does not ask for continuity keeps the behaviour
// it already had.
func extractSession(input map[string]any) string {
	for _, key := range sessionKeys {
		if text, ok := input[key].(string); ok && text != "" {
			return text
		}
	}
	return ""
}

// extractMessage pulls the user turn text out of the contract request input.
func extractMessage(input map[string]any) (string, error) {
	for _, key := range messageKeys {
		raw, ok := input[key]
		if !ok {
			continue
		}
		text, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("input field %q must be a string", key)
		}
		return text, nil
	}
	return "", fmt.Errorf("input must contain one of: message, input, prompt")
}

// newTurnFunc returns a turnFunc that opens a fresh conversation per request
// and runs a single turn. A conversation per request keeps requests isolated,
// which matches Agent Runtime's concurrent request model.
func newTurnFunc(
	packFile, agentName string, opts []sdk.Option, specs map[string]toolSpec,
	sessions *SessionStore,
) turnFunc {
	return func(ctx context.Context, _ string, input map[string]any) (any, error) {
		message, err := extractMessage(input)
		if err != nil {
			return nil, err
		}

		turnOpts, err := withSession(opts, sessions, extractSession(input))
		if err != nil {
			return nil, err
		}

		conv, err := sdk.Open(packFile, agentName, turnOpts...)
		if err != nil {
			return nil, fmt.Errorf("open conversation: %w", err)
		}
		warnUnsupportedTools(registerToolExecutors(conv, specs))

		resp, err := conv.Send(ctx, message)
		if err != nil {
			return nil, fmt.Errorf("send: %w", err)
		}

		return resp.Text(), nil
	}
}

// streamRequest carries the fixed inputs for one streaming turn.
type streamRequest struct {
	PackFile  string
	AgentName string
	Opts      []sdk.Option
	Input     map[string]any
	Specs     map[string]toolSpec
	Sessions  *SessionStore
}

// streamTurn runs one streaming turn, sending each text chunk to out. It
// returns the terminal error, or nil when the turn completes normally.
func streamTurn(ctx context.Context, req streamRequest, out chan<- string) error {
	message, err := extractMessage(req.Input)
	if err != nil {
		return err
	}

	turnOpts, err := withSession(req.Opts, req.Sessions, extractSession(req.Input))
	if err != nil {
		return err
	}

	conv, err := sdk.Open(req.PackFile, req.AgentName, turnOpts...)
	if err != nil {
		return fmt.Errorf("open conversation: %w", err)
	}
	warnUnsupportedTools(registerToolExecutors(conv, req.Specs))

	for chunk := range conv.Stream(ctx, message) {
		if chunk.Error != nil {
			return chunk.Error
		}
		if chunk.Type != sdk.ChunkText || chunk.Text == "" {
			continue
		}
		select {
		case out <- chunk.Text:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// newStreamFunc returns a streamFunc that opens a fresh conversation per
// request and streams the turn's text chunks.
func newStreamFunc(
	packFile, agentName string, opts []sdk.Option, specs map[string]toolSpec,
	sessions *SessionStore,
) streamFunc {
	return func(
		ctx context.Context, _ string, input map[string]any,
	) (<-chan string, <-chan error) {
		out := make(chan string)
		errCh := make(chan error, 1)

		req := streamRequest{
			PackFile:  packFile,
			AgentName: agentName,
			Opts:      opts,
			Input:     input,
			Specs:     specs,
			Sessions:  sessions,
		}

		go func() {
			defer close(out)
			defer close(errCh)

			if err := streamTurn(ctx, req, out); err != nil {
				errCh <- err
			}
		}()

		return out, errCh
	}
}

// withSession adds conversation persistence when the caller named a session.
//
// A request that names no session keeps the stateless behaviour every request
// had before. A request that names one when the runtime has no session storage
// is an error rather than a silent one-off turn: the caller asked for
// continuity, and quietly not providing it looks to them like an agent that
// forgets.
func withSession(
	opts []sdk.Option, sessions *SessionStore, sessionID string,
) ([]sdk.Option, error) {
	if sessionID == "" {
		return opts, nil
	}
	if sessions == nil {
		return nil, fmt.Errorf(
			"request names session %q but this runtime has no session storage: "+
				"%s was not set, so the engine's own sessions cannot be addressed",
			sessionID, envEngineID)
	}
	return append(opts,
		sdk.WithStateStore(sessions),
		sdk.WithConversationID(sessionID),
	), nil
}
