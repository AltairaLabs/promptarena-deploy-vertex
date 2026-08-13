# promptarena-deploy-vertex

[![CI](https://github.com/AltairaLabs/promptarena-deploy-vertex/workflows/CI/badge.svg)](https://github.com/AltairaLabs/promptarena-deploy-vertex/actions/workflows/ci.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_promptarena-deploy-vertex&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_promptarena-deploy-vertex)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_promptarena-deploy-vertex&metric=coverage)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_promptarena-deploy-vertex)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Google Agent Runtime deploy adapter for [PromptKit](https://github.com/AltairaLabs/PromptKit).

Agent Runtime was formerly Vertex AI Agent Engine. The API resource is still
`reasoningEngines` and the API host is still `aiplatform.googleapis.com`, which is
why this adapter is named `vertex`.

## Components

| Component | Purpose |
|---|---|
| `promptarena-deploy-vertex` | JSON-RPC deploy adapter plugin |
| `vertex-runtime` | Container entrypoint serving the Agent Runtime HTTP contract |

## Adapter configuration

```yaml
deploy:
  provider: vertex
  vertex:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    service_account: agent@my-project.iam.gserviceaccount.com
    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```

`providers` is required. The binding named `default` is the primary provider; if
none is named `default`, validation warns and names the binding that will be used.
Each binding sets **either** `arena_provider` (inheriting type and model from the
arena config) **or** `type` and `model` inline.

Labels are sanitized to GCP's rules (lowercase `[a-z0-9_-]`, 63 characters, keys
starting with a letter). Two keys that would sanitize to the same label are
rejected rather than silently merged.

### Adapter status

`get_provider_info` and `validate_config` are implemented. `plan`, `apply`,
`destroy`, `status` and `import` return a not-implemented error until the next
phase.

## Runtime contract

`vertex-runtime` listens on `0.0.0.0:8080` and serves:

| Route | Request | Response |
|---|---|---|
| `POST /api/reasoning_engine` | `{"class_method":"query","input":{"message":"…"}}` | `{"output":"…"}` |
| `POST /api/stream_reasoning_engine` | `{"class_method":"stream_query","input":{"message":"…"}}` | ndjson, one `{"output":"…"}` line per text chunk |

The input field may be `message`, `input`, or `prompt`. `class_method` defaults to
`query` and `stream_query` respectively when omitted.

The streaming route emits one line per token chunk and flushes after each. If the
turn fails after streaming has begun, the failure arrives as a trailing
`{"error":"…"}` line, because the `200` status has already been sent.

## Runtime environment variables

| Variable | Purpose |
|---|---|
| `PROMPTPACK_PACK_JSON` | Inline pack JSON |
| `PROMPTPACK_PACK_URI` | `gs://bucket/object` pack location, used when the pack is too large to inline |
| `PROMPTPACK_AGENT` | Which agent to serve; defaults to `agents.entry` or the pack's single prompt |
| `PROMPTPACK_PROVIDERS` | JSON list of resolved provider bindings |
| `GOOGLE_CLOUD_PROJECT` | GCP project for Vertex model routing |
| `GOOGLE_CLOUD_LOCATION` | GCP region for Vertex model routing |

One of `PROMPTPACK_PACK_JSON` or `PROMPTPACK_PACK_URI` is required.

### Provider bindings

`PROMPTPACK_PROVIDERS` is a JSON list. The binding named `default` is the primary
LLM; otherwise the first `llm`-role binding is used.

```json
[{"name":"default","role":"llm","type":"claude","model":"claude-sonnet-4@20250514"}]
```

Authentication is Application Default Credentials — inside Agent Runtime that is
the deployment's service account, so no API keys are involved. The service account
needs `roles/aiplatform.user`.

## Image

```
ghcr.io/altairalabs/promptkit-vertex-runtime:<version>
```

Agent Runtime cannot pull from `ghcr.io` directly. Configure an Artifact Registry
remote repository as a pull-through cache:

```bash
gcloud artifacts repositories create ghcr-remote \
  --repository-format=docker \
  --mode=remote-repository \
  --location=us-central1 \
  --remote-docker-repo=https://ghcr.io
```

Then reference the proxied path in your deploy config, for example
`us-central1-docker.pkg.dev/<project>/ghcr-remote/altairalabs/promptkit-vertex-runtime`.

This assumes the GHCR package is public. Private packages need upstream
credentials on the remote repository via Secret Manager, which is not supported yet.

## Development

```bash
make check          # fmt + lint + test + build
make build          # build vertex-runtime
make docker-build   # build the container image locally
```

Depends on published PromptKit modules — no sibling checkout required.

### Local smoke test

```bash
make build
PROMPTPACK_PACK_JSON="$(cat some.pack.json)" \
PROMPTPACK_PROVIDERS='[{"name":"default","role":"llm","type":"claude","model":"claude-sonnet-4@20250514"}]' \
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_LOCATION=us-central1 \
  ./vertex-runtime

curl -X POST http://localhost:8080/api/reasoning_engine \
  -H 'Content-Type: application/json' \
  -d '{"class_method":"query","input":{"message":"hello"}}'
```

## License

MIT
