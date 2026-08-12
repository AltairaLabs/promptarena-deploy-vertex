package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// Contract route paths, fixed by Agent Runtime.
const (
	routeUnary  = "/api/reasoning_engine"
	routeStream = "/api/stream_reasoning_engine"
)

// Class methods dispatched through the contract routes.
const (
	classMethodQuery       = "query"
	classMethodStreamQuery = "stream_query"
)

// contentTypeNDJSON is the media type for line-delimited JSON responses.
const contentTypeNDJSON = "application/x-ndjson"

// contractRequest is the Agent Runtime request envelope for both routes.
type contractRequest struct {
	ClassMethod string         `json:"class_method"`
	Input       map[string]any `json:"input"`
}

// contractResponse is the unary response envelope.
type contractResponse struct {
	Output any `json:"output"`
}

// streamErrorLine is the ndjson envelope used to report an error that occurs
// after the response headers have already been written.
type streamErrorLine struct {
	Error string `json:"error"`
}

// turnFunc executes one conversation turn for the named class method.
type turnFunc func(ctx context.Context, method string, input map[string]any) (any, error)

// streamFunc executes one streaming turn, returning a channel of text chunks
// and a channel carrying a terminal error. The text channel is closed when the
// turn completes; the error channel yields at most one error.
type streamFunc func(
	ctx context.Context, method string, input map[string]any,
) (<-chan string, <-chan error)

// decodeContractRequest reads and validates the request envelope, defaulting
// the class method when the caller omits it.
func decodeContractRequest(r *http.Request, defaultMethod string) (*contractRequest, error) {
	var req contractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	if req.ClassMethod == "" {
		req.ClassMethod = defaultMethod
	}
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	return &req, nil
}

// newUnaryHandler serves POST /api/reasoning_engine.
func newUnaryHandler(turn turnFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, err := decodeContractRequest(r, classMethodQuery)
		if err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		out, err := turn(r.Context(), req.ClassMethod, req.Input)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(contractResponse{Output: out}); err != nil {
			return
		}
	})
}

// newStreamHandler serves POST /api/stream_reasoning_engine, emitting one
// ndjson line per text chunk and flushing after each so callers see tokens as
// they arrive.
func newStreamHandler(stream streamFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, err := decodeContractRequest(r, classMethodStreamQuery)
		if err != nil {
			http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		chunks, errs := stream(r.Context(), req.ClassMethod, req.Input)
		writeStream(w, chunks, errs)
	})
}

// writeStream drains the chunk channel to the response as ndjson. Errors
// arriving before the first chunk become an HTTP error; errors after that
// become a trailing ndjson line, because the status has already been sent.
func writeStream(w http.ResponseWriter, chunks <-chan string, errs <-chan error) {
	flusher, canFlush := w.(http.Flusher)
	enc := json.NewEncoder(w)
	wroteHeader := false

	for text := range chunks {
		if !wroteHeader {
			w.Header().Set("Content-Type", contentTypeNDJSON)
			w.WriteHeader(http.StatusOK)
			wroteHeader = true
		}
		if err := enc.Encode(contractResponse{Output: text}); err != nil {
			return
		}
		if canFlush {
			flusher.Flush()
		}
	}

	err := <-errs
	if err == nil {
		if !wroteHeader {
			w.Header().Set("Content-Type", contentTypeNDJSON)
			w.WriteHeader(http.StatusOK)
		}
		return
	}

	if !wroteHeader {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_ = enc.Encode(streamErrorLine{Error: err.Error()})
	if canFlush {
		flusher.Flush()
	}
}
