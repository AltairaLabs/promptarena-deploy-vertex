// Package main implements the vertex-runtime binary, the container entrypoint
// that serves a PromptKit pack over Google Agent Runtime's HTTP contract.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// packDir is where the resolved pack file is written inside the container.
const packDir = "/tmp"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(context.Background(), log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	packFile, err := resolvePackFile(ctx, cfg, packDir, fetchGCS)
	if err != nil {
		return fmt.Errorf("resolve pack: %w", err)
	}

	pack, err := prompt.LoadPack(packFile)
	if err != nil {
		return fmt.Errorf("load pack: %w", err)
	}

	agentName, err := resolveAgentName(cfg, pack)
	if err != nil {
		return err
	}

	opts, err := buildSDKOptions(cfg)
	if err != nil {
		return fmt.Errorf("provider bindings: %w", err)
	}

	shutdownTracing, traceOpts := setupTracing(cfg, log)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if shutdownErr := shutdownTracing(shutdownCtx); shutdownErr != nil {
			log.Error("tracing shutdown", "error", shutdownErr)
		}
	}()
	opts = append(opts, traceOpts...)

	log.Info("runtime configured",
		"agent", agentName,
		"pack", packFile,
		"project", cfg.Project,
		"location", cfg.Location,
		"provider_options", len(opts))

	specs, err := parseToolSpecs(cfg.ToolSpecsJSON)
	if err != nil {
		return fmt.Errorf("tool specs: %w", err)
	}
	if len(specs) > 0 {
		log.Info("tool executors configured", "count", len(specs))
	}

	mux := buildMux(
		newTurnFunc(packFile, agentName, opts, specs),
		newStreamFunc(packFile, agentName, opts, specs),
	)
	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)

	return runServer(ctx, log, addr, mux)
}
