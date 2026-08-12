// Package main implements the promptarena-deploy-vertex binary, a Google Agent
// Runtime deploy adapter for PromptKit.
package main

import (
	"fmt"
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/deploy/adaptersdk"

	"github.com/AltairaLabs/promptarena-deploy-vertex/internal/vertex"
)

func main() {
	provider := vertex.NewProvider()
	if err := adaptersdk.Serve(provider); err != nil {
		fmt.Fprintf(os.Stderr, "vertex: %v\n", err)
		os.Exit(1)
	}
}
