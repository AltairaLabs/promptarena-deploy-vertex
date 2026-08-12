package vertex

import "fmt"

// knownLocations lists the regions Agent Runtime was available in when this
// adapter was released. An unrecognized location is a warning, never an error:
// Google adds regions faster than this adapter ships, and hard-failing on a
// newly added region would be worse than letting the API reject it.
var knownLocations = map[string]bool{
	"us-central1":     true,
	"us-east4":        true,
	"us-west1":        true,
	"europe-west1":    true,
	"europe-west2":    true,
	"europe-west3":    true,
	"europe-west4":    true,
	"asia-east1":      true,
	"asia-northeast1": true,
	"asia-southeast1": true,
}

// diagnoseConfig returns non-blocking advisories about a structurally valid
// config. These catch misconfigurations that would otherwise surface as an
// opaque failure minutes into an apply.
func diagnoseConfig(cfg *Config) []string {
	var warnings []string

	if cfg.Location != "" && !knownLocations[cfg.Location] {
		warnings = append(warnings, fmt.Sprintf(
			"location %q is not in this adapter's list of known Agent Runtime regions; "+
				"if it is newly supported this warning is safe to ignore", cfg.Location))
	}

	if cfg.ServiceAccount == "" {
		warnings = append(warnings,
			"service_account is not set; the engine will run as the project's default "+
				"compute service account, which usually lacks roles/aiplatform.user")
	}

	if cfg.ImageMode == ImageModeCloudBuild && cfg.DockerfilePath != "" {
		warnings = append(warnings,
			"dockerfile_path bypasses the generated Dockerfile, so honoring the Agent "+
				"Runtime contract (listen on 0.0.0.0:8080, serve /api/reasoning_engine and "+
				"/api/stream_reasoning_engine) becomes your responsibility")
	}

	return warnings
}
