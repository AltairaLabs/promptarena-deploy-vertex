package vertex

import (
	"encoding/json"

	"github.com/AltairaLabs/promptarena/v2/deploy"
	"github.com/AltairaLabs/promptarena/v2/deploy/adaptersdk"
)

// configSchema is the JSON Schema (draft-07) for the vertex provider config.
// It mirrors Config in config.go and ProviderBinding in bindings.go. The
// runtime_override block comes from the deploy SDK, so it reads the same in
// every adapter's schema.
var configSchema = withRuntimeOverride(baseConfigSchema)

// withRuntimeOverride adds the SDK's runtime_override schema to a config
// schema. Both inputs are constants of this package, so a failure is a
// programming error and panics at start-up, where a test sees it.
func withRuntimeOverride(schema string) string {
	var doc map[string]any
	if err := json.Unmarshal([]byte(schema), &doc); err != nil {
		panic("vertex: config schema is not JSON: " + err.Error())
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		panic("vertex: config schema has no properties")
	}
	props[deploy.RuntimeOverrideConfigKey] = adaptersdk.RuntimeOverrideSchema()
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		panic("vertex: config schema does not marshal: " + err.Error())
	}
	return string(out)
}

// baseConfigSchema is the schema for every field the adapter defines itself.
const baseConfigSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["project", "location", "providers"],
  "properties": {
    "project": {
      "type": "string",
      "description": "GCP project ID hosting the Agent Runtime deployment"
    },
    "location": {
      "type": "string",
      "description": "GCP region, e.g. us-central1"
    },
    "staging_bucket": {
      "type": "string",
      "description": "gs:// bucket for staged packs"
    },
    "image_mode": {
      "type": "string",
      "enum": ["prebuilt", "cloudbuild"],
      "description": "prebuilt references a published image; cloudbuild is not implemented and is refused"
    },
    "image": {
      "type": "string",
      "description": "Artifact Registry reference to the runtime image (required in prebuilt mode)"
    },
    "runtime_binary_path": {
      "type": "string",
      "description": "Unused: only applied to the unimplemented cloudbuild mode"
    },
    "dockerfile_path": {
      "type": "string",
      "description": "Unused: only applied to the unimplemented cloudbuild mode"
    },
    "service_account": {
      "type": "string",
      "description": "Service account the deployed engine runs as"
    },
    "resource_limits": {
      "type": "object",
      "properties": {
        "cpu": { "type": "string" },
        "memory": { "type": "string" }
      },
      "additionalProperties": false
    },
    "min_instances": { "type": "integer", "minimum": 0 },
    "max_instances": { "type": "integer", "minimum": 1 },
    "container_concurrency": { "type": "integer", "minimum": 1 },
    "env": {
      "type": "object",
      "additionalProperties": { "type": "string" },
      "description": "Additional container environment variables; PROMPTPACK_* names are reserved"
    },
    "labels": {
      "type": "object",
      "additionalProperties": { "type": "string" },
      "description": "Labels applied to created resources; sanitized to GCP label rules"
    },
    "observability": {
      "type": "object",
      "properties": {
        "tracing_enabled": {
          "type": "boolean",
          "description": "Emit OTel traces, including eval scores, from the deployed engine"
        },
        "otlp_endpoint": {
          "type": "string",
          "description": "OTLP collector endpoint; required when tracing_enabled is true"
        }
      },
      "additionalProperties": false
    },
    "pack_inline_limit_bytes": {
      "type": "integer",
      "minimum": 1,
      "description": "Serialized pack size above which the pack is staged to GCS"
    },
    "dry_run": {
      "type": "boolean",
      "description": "When true, Apply simulates resource creation without calling GCP"
    },
    "providers": {
      "type": "array",
      "minItems": 1,
      "description": "Provider bindings; the binding named default is primary",
      "items": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name": { "type": "string" },
          "role": {
            "type": "string",
            "enum": ["llm", "embedding", "tts", "stt", "image", "inference"]
          },
          "arena_provider": {
            "type": "string",
            "description": "Inherit type and model from this arena provider id"
          },
          "type": { "type": "string" },
          "model": { "type": "string" },
          "vertex_endpoint": {
            "type": "string",
            "description": "Model Garden endpoint id, or openapi for the shared MaaS endpoint"
          }
        },
        "additionalProperties": false
      }
    }
  },
  "additionalProperties": false
}`
