---
title: Configure the adapter
description: Every configuration option for the Google Agent Runtime deploy adapter
---

The adapter is configured through the `deploy.config` section of your arena
configuration. This guide covers every option and the errors each one produces.

## Minimal configuration

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```

`project`, `location` and `providers` are always required. `image` is required
in the default `prebuilt` image mode.

## Provider bindings

`providers` tells the deployed engine which model to call. It is required — an
engine with no LLM binding cannot answer anything.

```yaml
providers:
  # Inherit type and model from a provider already defined in the arena config
  - name: default
    role: llm
    arena_provider: gemini-flash

  # Or state them inline
  - name: fallback
    role: llm
    type: gemini
    model: gemini-2.5-pro
```

Each binding sets **either** `arena_provider` **or** `type` and `model` — not
both.

The binding named `default` is the primary LLM. If no binding is named
`default`, validation warns and names the binding that will actually be used —
it does not fail, because the first `llm`-role binding is a reasonable guess,
but a warning you ignore here becomes an agent calling the wrong model.

Authentication is Application Default Credentials throughout. Inside Agent
Runtime that is the deployment's service account, so no API keys are involved.

## Identity

```yaml
service_account: agent@my-project.iam.gserviceaccount.com
```

The service account the engine runs as. It needs `roles/aiplatform.user` to call
models.

If you omit it, the engine runs as the Reasoning Engine Service Agent, which
usually lacks that role — the adapter warns at plan time rather than failing,
because some projects do grant it.

## Image

```yaml
image_mode: prebuilt          # default
image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
```

Agent Runtime cannot pull from `ghcr.io`. Point `image` at Artifact Registry —
either a remote repository proxying GHCR, or an image you pushed directly.

:::danger[`cloudbuild` mode is not implemented]
`image_mode: cloudbuild` passes validation and accepts `runtime_binary_path`,
`dockerfile_path` and `staging_bucket`, but `apply` always deploys `image`. In
cloudbuild mode `image` is typically empty, so the create fails. Use `prebuilt`.
:::

## Scaling

```yaml
resource_limits:
  cpu: "2"
  memory: 4Gi
min_instances: 0
max_instances: 10
container_concurrency: 8
```

All optional; omitting a field leaves Google's default in place.

Validation rejects a negative `min_instances`, a `max_instances` below 1, a
`container_concurrency` below 1, and a `min_instances` greater than
`max_instances`.

`min_instances` above zero bills continuously, per engine. A multi-agent pack
multiplies that by the number of agents.

## Labels

```yaml
labels:
  team: platform
  env: production
```

Labels are sanitized to GCP's rules: lowercase `[a-z0-9_-]`, 63 characters, keys
starting with a letter. Two keys that would sanitize to the same label are
**rejected** rather than silently merged — a merge would drop one of them
without saying so.

## Pack delivery

```yaml
pack_inline_limit_bytes: 24576   # default
staging_bucket: gs://my-bucket
```

Packs at or below the limit ride in an environment variable. Larger packs are
staged to Cloud Storage and appear in the plan as a `pack_object` resource. The
default leaves headroom under the Cloud Run substrate's ~32 KiB environment cap.

`staging_bucket` must start with `gs://`.

## Observability

```yaml
observability:
  tracing_enabled: true
  otlp_endpoint: http://collector:4318
```

Off by default. `otlp_endpoint` must be a **full URL including the scheme** — a
`host:port` value is rejected, because the exporter would silently build
`http:///v1/traces` and drop every span while the deployment looked healthy.

See [tracing](./observability/) for what actually shows up.

## Dry run

```yaml
dry_run: true
```

Plans and applies without calling Google APIs. See [dry run](./dry-run/).

`dry_run` deliberately does **not** affect the config hash: it changes adapter
behavior, not deployed state, so toggling it must not show up as a diff.

## Full example

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    service_account: agent@my-project.iam.gserviceaccount.com

    resource_limits:
      cpu: "2"
      memory: 4Gi
    max_instances: 10
    container_concurrency: 8

    labels:
      team: platform
      env: production

    observability:
      tracing_enabled: true
      otlp_endpoint: http://collector.observability.svc:4318

    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```

Field-by-field types and defaults are in the
[configuration reference](/reference/configuration/).
