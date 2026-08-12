// Package main implements the vertex-runtime binary, the container entrypoint
// that serves a PromptKit pack over Google Agent Runtime's HTTP contract.
package main

import (
	"log/slog"
	"os"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	log.Info("vertex-runtime starting", "version", Version)
}
