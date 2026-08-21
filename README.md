# promptarena-deploy-vertex

[![CI](https://github.com/AltairaLabs/promptarena-deploy-vertex/workflows/CI/badge.svg)](https://github.com/AltairaLabs/promptarena-deploy-vertex/actions/workflows/ci.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_promptarena-deploy-vertex&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_promptarena-deploy-vertex)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=AltairaLabs_promptarena-deploy-vertex&metric=coverage)](https://sonarcloud.io/summary/new_code?id=AltairaLabs_promptarena-deploy-vertex)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Google Agent Runtime deploy adapter for [PromptKit](https://github.com/AltairaLabs/PromptKit).

Agent Runtime was formerly Vertex AI Agent Engine. The API resource is still
`reasoningEngines` and the API host is still `aiplatform.googleapis.com`, which is
why this adapter is named `vertex`.

Full documentation lives in [`docs/`](docs/) and is published into the
PromptArena documentation site. Run it standalone with
`npm --prefix docs install && npm --prefix docs run dev`.

## Components

| Component | Purpose |
|---|---|
| `promptarena-deploy-vertex` | JSON-RPC deploy adapter plugin |
| `vertex-runtime` | Container entrypoint serving the Agent Runtime HTTP contract |

## Releasing

Pushing a `v*` tag runs GoReleaser, which builds the adapter for
linux/darwin × amd64/arm64 and opens a **draft** release. The assets are bare
binaries named `promptarena-deploy-vertex_<goos>_<goarch>`, which is exactly
what `promptarena deploy adapter install vertex` downloads — the installer
builds that name from `runtime.GOOS` and `runtime.GOARCH`, so archives or
title-cased names would not resolve.

Publishing the draft fires `publish-runtime.yml`, which pushes the runtime
container image to `ghcr.io/altairalabs/promptkit-vertex-runtime` at the same
tag. That ordering is deliberate: the image is only published once you have
accepted the release.

```bash
git tag v0.1.0
git push origin v0.1.0
# review the draft release, then publish it to trigger the image build
```

## Adapter configuration

```yaml
deploy:
  provider: vertex
  config:
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

| Method | Status |
|---|---|
| `get_provider_info` | Implemented |
| `validate_config` | Implemented |
| `plan` | Implemented |
| `apply` | Implemented |
| `destroy` | Not implemented |
| `status` | Not implemented |
| `import` | Not implemented |

### Deploying: prerequisites

`apply` creates one Agent Runtime engine per pack agent from the configured
container image. Two things must be in place first, both of which fail *after*
the engine is created rather than at validation time:

**1. The runtime image must live in Artifact Registry.** Agent Runtime cannot
pull from `ghcr.io` directly — use an AR remote repository as a pull-through
cache, or push the image to AR directly.

**2. The Reasoning Engine Service Agent needs read access to it:**

```bash
gcloud artifacts repositories add-iam-policy-binding <REPO> \
  --location=<LOCATION> --project=<PROJECT> \
  --member="serviceAccount:service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader
```

Without it the create succeeds and the engine then fails to start with an image
access error.

If `service_account` is unset, the engine runs as the Reasoning Engine Service
Agent, which needs `roles/aiplatform.user` to call models.

### Reserved environment variables

Agent Runtime **reserves** `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION`
and rejects a deployment that sets them — it injects them into the container
itself. The adapter therefore passes `PROMPTPACK_PROJECT` and
`PROMPTPACK_LOCATION`, and the runtime reads the prefixed names first, falling
back to the conventional ones. The same image runs unchanged on hosts that
inject nothing.

## Observability

Tracing is **off by default** — an unconfigured deployment sends nothing and
pays nothing. Enable it in the deploy config:

```yaml
deploy:
  provider: vertex
  config:
    observability:
      tracing_enabled: true
      otlp_endpoint: http://collector:4318
```

`otlp_endpoint` must be a **full URL including the scheme**. The exporter builds
its target with `otlptracehttp.WithEndpointURL`, so a `host:port` value produces
`http:///v1/traces` — no host — and every export fails while the deployment looks
healthy. Validation rejects it.

The adapter injects `PROMPTPACK_TRACING_ENABLED` and the standard
`OTEL_EXPORTER_OTLP_ENDPOINT`, so the same image works with any OTLP collector.

### What was actually observed

Verified against a local `otel/opentelemetry-collector-contrib`:

```
Resource attributes:
  -> service.name: Str(vertex-runtime)
InstrumentationScope github.com/AltairaLabs/PromptKit 1.0.0
Span: gemini chat
  -> gen_ai.operation.name: Str(chat)
  -> gen_ai.system: Str(gemini)
  -> gen_ai.request.model: Str(gemini-2.5-flash)
  -> gen_ai.usage.input_tokens: Int(0)
  -> promptkit.message.count: Int(1)
  -> promptkit.tool.count: Int(0)
```

Eval results reach telemetry too, when the pack declares them:

```
Attributes:
  -> gen_ai.evaluation.name: Str(response-length)
  -> promptkit.eval.type: Str(max_length)
  -> promptkit.guardrail: Bool(false)
  -> gen_ai.evaluation.score: Double(1)
  -> gen_ai.evaluation.explanation: Str(length 22, max 500)
```

### Evals are traced; guardrails are not

This distinction matters and is easy to trip over.

An **eval** — declared in the pack's `evals` section — runs through the eval
runner, which emits `EventEvalCompleted`. The telemetry listener turns that into
the span attributes above.

A **guardrail** — a `validators` entry on a prompt — computes using the same eval
handlers but runs through the guardrail hook adapter, which emits **no event**. A
firing guardrail rewrites the response and leaves no trace attribute behind. The
listener even has a `promptkit.guardrail` flag for the distinction; nothing
currently sets it true.

So a pack with only `validators` produces provider spans but no evaluation
scores. Declare an `evals` section to see scores:

```json
"evals": [
  {
    "id": "response-length",
    "type": "max_length",
    "trigger": "every_turn",
    "params": { "max_characters": 500 }
  }
]
```

### Not verified

Whether Google Cloud Trace accepts OTLP directly from an Agent Runtime
container, and on what host and credentials, is **untested**. Point
`otlp_endpoint` at a collector you control.

### A2A

Agent cards are generated but **not yet attached**: `ReasoningEngineSpec.agentCard`
exists in the v1beta1 REST API but is absent from the published protos, so the Go
client cannot set it. See issue #3.

`plan` performs no GCP calls: it diffs the pack and config hashes against prior
adapter state and reports the resource changes a deploy would make.

### How plan decides

Three inputs drive the diff:

| Input | Effect |
|---|---|
| Pack hash | Any pack change updates every engine |
| Config hash | Project, location, image, service account, scaling, labels and resolved provider bindings |
| Prior state | Engines absent from state are created; engines whose agent left the pack are deleted |

An engine whose previous creation did not finish is recorded as in-flight and is
reconciled on the next apply rather than orphaned.

`dry_run` deliberately does **not** affect the config hash — it changes adapter
behavior, not deployed state.

Packs above `pack_inline_limit_bytes` (24576 by default) are delivered through
Cloud Storage instead of an environment variable, which appears in the plan as a
`pack_object` resource.

`plan` warns, without failing, when a pack declares `a2a__` tools (agent-to-agent
calls are not wired yet) or when a multi-agent pack will deploy as independent
engines with no routing between them.

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
| `PROMPTPACK_TOOL_SPECS` | JSON map of tool name to execution config; absent when the arena declares no tools |
| `GOOGLE_CLOUD_PROJECT` | GCP project for Vertex model routing |
| `GOOGLE_CLOUD_LOCATION` | GCP region for Vertex model routing |

One of `PROMPTPACK_PACK_JSON` or `PROMPTPACK_PACK_URI` is required.

### Tools

A compiled pack carries a tool's *schema* — name, description, parameters — but
not how to run it. Execution config lives in the arena config under `tool_specs`,
which the CLI passes to the adapter, and which the adapter forwards to the engine
as `PROMPTPACK_TOOL_SPECS`. **Without an arena config, a deployed agent advertises
its tools to the model and then has nothing to fulfill the calls** — the model
apologizes instead of answering.

Supported modes:

| Mode | Support |
|---|---|
| `mock` | Full. `mock_result` and `mock_template`, with the same semantics as `promptarena run`: a rendered template is parsed back as JSON, falling back to `{"result": "<text>"}`. |
| `live` | HTTP `url` and `method` only. Headers, timeouts and request/response mapping are not forwarded yet. |
| `mcp`, `exec`, `client` | Not supported. Each needs a resource the container does not have — an MCP server, a subprocess, or a client on the other end of the connection. |

Tools in an unsupported mode are logged once at startup and left unregistered,
rather than failing the deployment: the rest of the agent still works.

Changing a tool spec changes the plan's config hash, so editing a `mock_result`
shows up as an update rather than silently leaving the old value deployed.

Verified against a deployed engine: with a `mock_template` returning a status
string that appears nowhere in the prompt, `gemini-2.5-flash` calls the tool and
answers with the tool's value (`test/integration/deployed_test.go`).

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
