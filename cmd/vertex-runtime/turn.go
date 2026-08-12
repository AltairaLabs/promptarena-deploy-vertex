package main

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// messageKeys are the accepted input field names for the user turn, in
// precedence order. "prompt" and "input" match common hyperscaler conventions;
// "message" is the canonical PromptKit name.
var messageKeys = []string{"message", "input", "prompt"}

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
func newTurnFunc(packFile, agentName string, opts []sdk.Option) turnFunc {
	return func(ctx context.Context, _ string, input map[string]any) (any, error) {
		message, err := extractMessage(input)
		if err != nil {
			return nil, err
		}

		conv, err := sdk.Open(packFile, agentName, opts...)
		if err != nil {
			return nil, fmt.Errorf("open conversation: %w", err)
		}

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
}

// streamTurn runs one streaming turn, sending each text chunk to out. It
// returns the terminal error, or nil when the turn completes normally.
func streamTurn(ctx context.Context, req streamRequest, out chan<- string) error {
	message, err := extractMessage(req.Input)
	if err != nil {
		return err
	}

	conv, err := sdk.Open(req.PackFile, req.AgentName, req.Opts...)
	if err != nil {
		return fmt.Errorf("open conversation: %w", err)
	}

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
func newStreamFunc(packFile, agentName string, opts []sdk.Option) streamFunc {
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
