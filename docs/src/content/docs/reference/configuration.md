---
title: Configuration reference
description: Every field in the vertex deploy config, with types, defaults and validation rules
---

Fields of `deploy.config` when `deploy.provider` is `vertex`. The adapter
publishes this as a JSON Schema (draft-07) through `get_provider_info`;
unknown fields are rejected.

## Required

| Field | Type | Description |
|---|---|---|
| `project` | string | GCP project ID hosting the deployment. |
| `location` | string | GCP region, e.g. `us-central1`. |
| `providers` | array | Provider bindings. At least one. |

## Image

| Field | Type | Default | Description |
|---|---|---|---|
| `image_mode` | string | `prebuilt` | `prebuilt` or `cloudbuild`. |
| `image` | string | — | Artifact Registry reference to the runtime image. Required in `prebuilt` mode. |
| `runtime_binary_path` | string | — | Go binary to containerize (`cloudbuild` mode). |
| `dockerfile_path` | string | — | User-supplied Dockerfile (`cloudbuild` escape hatch). |

:::danger[`cloudbuild` is not implemented]
`image_mode: cloudbuild` validates and produces diagnostics, but `apply` always
deploys `image` — it never builds anything. Use `prebuilt`.
:::

Validation:

- `prebuilt` without `image` → `image is required in prebuilt mode`
- `cloudbuild` without `runtime_binary_path` or `dockerfile_path` →
  `cloudbuild mode requires runtime_binary_path or dockerfile_path`
- `cloudbuild` without `staging_bucket` → `staging_bucket is required in cloudbuild mode`
- Any other value → `image_mode "x" must be "prebuilt" or "cloudbuild"`

## Identity

| Field | Type | Default | Description |
|---|---|---|---|
| `service_account` | string | — | Service account the engine runs as. Needs `roles/aiplatform.user`. |

Unset produces a diagnostic warning, not an error: the engine falls back to the
Reasoning Engine Service Agent, which usually lacks that role.

## Scaling

| Field | Type | Default | Description |
|---|---|---|---|
| `resource_limits.cpu` | string | Google default | e.g. `"2"`. |
| `resource_limits.memory` | string | Google default | e.g. `4Gi`. |
| `min_instances` | integer | Google default | Minimum 0. Above zero bills continuously. |
| `max_instances` | integer | Google default | Minimum 1. |
| `container_concurrency` | integer | Google default | Minimum 1. |

Validation rejects a negative `min_instances`, a `max_instances` below 1, a
`container_concurrency` below 1, and `min_instances` greater than
`max_instances`.

Omitting `resource_limits` entirely leaves Google's default; setting the object
with neither field is treated as unset.

## Labels

| Field | Type | Default | Description |
|---|---|---|---|
| `labels` | object of string | — | Applied to created resources. |

Sanitized to GCP rules: lowercase `[a-z0-9_-]`, 63 characters, keys starting
with a letter. Two keys that sanitize to the same label are **rejected**.

## Pack delivery

| Field | Type | Default | Description |
|---|---|---|---|
| `pack_inline_limit_bytes` | integer | `24576` | Serialized pack size above which the pack is staged to Cloud Storage. Minimum 1. |
| `staging_bucket` | string | — | `gs://` bucket for staged packs. Must start with `gs://`. |

## Observability

| Field | Type | Default | Description |
|---|---|---|---|
| `observability.tracing_enabled` | boolean | `false` | Emit OTel traces, including eval scores. |
| `observability.otlp_endpoint` | string | — | OTLP collector endpoint. Required when tracing is enabled. |

Validation:

- `tracing_enabled: true` with no endpoint →
  `observability.otlp_endpoint is required when observability.tracing_enabled is true`
- An endpoint without `http://` or `https://` →
  `must be a full URL including scheme (for example http://collector:4318), not host:port`

The second rule exists because the exporter would otherwise build
`http:///v1/traces` and drop every span while the deployment looked healthy.

## Behavior

| Field | Type | Default | Description |
|---|---|---|---|
| `dry_run` | boolean | `false` | Simulate resource creation without calling GCP. |

Excluded from the config hash by design — it changes adapter behavior, not
deployed state.

## Provider bindings

Each entry of `providers`:

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Binding name. `default` is the primary LLM. |
| `role` | string | no | One of `llm`, `embedding`, `tts`, `stt`, `image`, `inference`. |
| `arena_provider` | string | no | Inherit type and model from this arena provider id. |
| `type` | string | no | Provider type, when stated inline. |
| `model` | string | no | Model id, when stated inline. |
| `vertex_endpoint` | string | no | Model Garden endpoint id, or `openapi` for the shared MaaS endpoint. |

Set **either** `arena_provider` **or** `type` and `model`.

If no binding is named `default`, validation warns and names the binding that
will be used — the first with role `llm`. A pack deployed with no `llm` binding
at all is an error: the engine would have no model to call.

## Example

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    service_account: agent-runtime@my-project.iam.gserviceaccount.com

    resource_limits:
      cpu: "2"
      memory: 4Gi
    max_instances: 10
    container_concurrency: 8

    labels:
      team: platform

    observability:
      tracing_enabled: true
      otlp_endpoint: http://collector:4318

    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```
