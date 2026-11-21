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
linux/darwin × amd64/arm64 and publishes the release, not a draft
(`draft: false` in `.goreleaser.yml`): `promptarena deploy adapter install
vertex` downloads the assets anonymously, and a draft's assets are visible only
to users with push access. The assets are bare
binaries named `promptarena-deploy-vertex_<goos>_<goarch>`, which is exactly
what the installer downloads — it builds that name from `runtime.GOOS` and
`runtime.GOARCH`, so archives or title-cased names would not resolve.

Once GoReleaser succeeds, the same Release workflow calls `publish-runtime.yml`,
which builds the runtime container image from the tag and pushes it to
`ghcr.io/altairalabs/promptkit-vertex-runtime:vX.Y.Z`. It is called directly
rather than triggered by `release: published`, because GoReleaser publishes
with the workflow's `GITHUB_TOKEN` and GitHub starts no workflow from an event
that token caused.

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

`:latest` moves only when the tag being built is the newest stable `vX.Y.Z`
tag, so a prerelease never takes it.

To rebuild or backfill the image for an existing tag, dispatch the workflow. It
checks out the tag it is given, so it can be dispatched from `main`, and
rebuilding an older tag leaves `:latest` where it is:

```bash
gh workflow run publish-runtime.yml -f tag=vX.Y.Z
```

## Adapter configuration

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    service_account: agent-runtime@my-project.iam.gserviceaccount.com
    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```

`image` is required unless [`runtime_override`](#run-another-container) names
one. `providers` is required. The binding named `default` is the primary provider; if
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
| `destroy` | Implemented |
| `status` | Implemented |
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
| Pack hash | Any pack change updates every engine. `compilation.created_at` is ignored, since every compile stamps a new one |
| Config hash | Project, location, image, service account, scaling, labels and resolved provider bindings |
| Prior state | Engines absent from state are created; engines whose agent left the pack are deleted |
| `rollout_stage` | Each update's detail says where the traffic goes. `promote` plans a traffic change per engine and nothing else |

An engine whose previous creation did not finish is recorded as in-flight and is
reconciled on the next apply rather than orphaned.

An apply that fails partway returns the full state: the engines it created (an
interrupted create as in-flight) plus every prior engine it did not reach or
failed to update, with the hashes cleared so the next plan updates every engine.
The adapter advertises `partial_apply_state`, so a PromptArena CLI that supports
it saves that state and the next apply picks up where the failed one stopped.

`dry_run` deliberately does **not** affect the config hash — it changes adapter
behavior, not deployed state.

Packs above `pack_inline_limit_bytes` (24576 by default) are delivered through
Cloud Storage instead of an environment variable, which appears in the plan as a
`pack_object` resource.

`plan` warns, without failing, when a pack declares `a2a__` tools (agent-to-agent
calls are not wired yet) or when a multi-agent pack will deploy as independent
engines with no routing between them.

## Staged rollouts

Each apply that changes an engine's spec creates a new Agent Runtime **runtime
revision**. Revisions are immutable, and the engine's traffic configuration
decides which of them serve its `:query` and `:streamQuery`. By default every
request goes to the latest revision, so a deploy serves the new version as soon
as it is ready. `rollout_stage` stages a version first and moves the traffic as
a separate step.

| `rollout_stage` | What apply does |
|---|---|
| `live` (default) | Updates the engine and serves the new revision at 100%. An engine left on a manual split has the split moved to the new revision. |
| `candidate` | Pins the traffic to the revision serving now at 100%, **then** updates the engine. The new revision is staged with 0% (or `candidate_percent`) and recorded in state as the candidate. |
| `promote` | Sends no spec. Moves 100% of the traffic to the staged candidate. Refused when nothing is staged. |

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    rollout_stage: candidate   # then: promote
    candidate_percent: 10      # optional: 10% to the candidate, 90% stays
    providers:
      - name: default
        type: gemini
        model: gemini-2.5-flash
```

A typical sequence: apply with `candidate`, call the candidate directly at
`https://<location>-aiplatform.googleapis.com/v1beta1/<engine>/runtimeRevisions/<id>:query`
(the same request body as the engine's `:query`), then apply with `promote`.
`rollout_stage` and `candidate_percent` are not part of the config hash, so
switching between them does not make plan report a config change. A promote
serves the candidate exactly as it was staged. If the pack or config changed
after it was staged, plan warns you.

The first deploy of an engine has nothing else to serve, so its first revision
takes the traffic whatever the stage.

**Identifying a revision.** A runtime revision has no labels, only its name,
spec, create time and state. So the adapter writes the pack's `version` into
every revision's spec as the container variable `PROMPTPACK_VERSION`. To read it
back, look for `spec.deploymentSpec.env[name=PROMPTPACK_VERSION]` on
`GET v1beta1/<engine>/runtimeRevisions`. `status` reports the live split with
each revision's pack version, for example
`traffic: 1 (v1.0.0) 100%; staged candidate 2 (v1.1.0)`.

Things to know:

- Traffic splitting and runtime revisions are a **preview** feature of the
  v1beta1 API. The published Go client does not model them, so the adapter makes
  these calls over REST with the same credentials.
- A revision with 0% of the traffic still answers when it is called directly.
  In testing it kept one warm instance, not `min_instances`, until it was given
  traffic or deleted. Agent Runtime garbage-collects revisions with no traffic
  beyond the engine's retention count.
- **Changing an engine's Gateway binding archives every revision.** That
  includes the staged candidate, so promote has nothing to move to afterwards.
  Do not change the binding while a candidate is staged.

## Runtime contract

`vertex-runtime` listens on `0.0.0.0:8080` and serves:

| Route | Request | Response |
|---|---|---|
| `POST /api/reasoning_engine` | `{"class_method":"query","input":{"message":"…"}}` | `{"output":"…"}` |
| `POST /api/stream_reasoning_engine` | `{"class_method":"stream_query","input":{"message":"…"}}` | ndjson, one `{"output":"…"}` line per text chunk |

The input field may be `message`, `input`, or `prompt`. `class_method` defaults to
`query` and `stream_query` respectively when omitted.

A request continues a conversation by naming a session in `input` —
`session_id`, `sessionId` or `session`, in that order of precedence. History is
stored in the engine's own Agent Runtime sessions, so it survives restarts and is
shared across replicas. Omitting the session gives a one-off turn. Naming one
when the runtime has no session storage (no `GOOGLE_CLOUD_AGENT_ENGINE_ID`, or no
project or location) is an error, not a silent one-off turn.

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
| `PROMPTPACK_VERSION` | The pack's `version`. The runtime does not read it; it is there so each runtime revision records which pack version it runs. Absent when the pack has no version |
| `GOOGLE_CLOUD_PROJECT` | GCP project for Vertex model routing |
| `GOOGLE_CLOUD_LOCATION` | GCP region for Vertex model routing |
| `GOOGLE_CLOUD_AGENT_ENGINE_ID` | The engine's own ID, which addresses its sessions. Expected from Agent Runtime, not set by the adapter; without it, session storage is off |

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
| `live` | `http.url` (required), `method`, `headers`, `headers_from_env`, `timeout_ms` and `redact`. `headers_from_env` entries are `Header-Name=ENV_VAR`, read on the engine at call time, so the variable must be set there (the deploy config's `env` block). Request/response mapping (`request`, `response`, `query_params`, `header_params`, `body_mapping`) and `multimodal` are ignored. |
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

## Run another container

`runtime_override` deploys a container of your choosing in place of
`vertex-runtime`. The adapter still creates one engine per agent and still sets
the pack environment (`PROMPTPACK_PACK_JSON` or `PROMPTPACK_PACK_URI`,
`PROMPTPACK_AGENT` and the rest of [Runtime environment
variables](#runtime-environment-variables)), so the container can read the pack
however it was deployed.

| Field | On Agent Runtime |
|---|---|
| `image` | Replaces `image`, which may then be left out. Same Artifact Registry rules as the [runtime image](#image) |
| `env` | Added to the engine's environment. A name may not also appear in `env`, start with `PROMPTPACK_`, or be `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION` or `OTEL_EXPORTER_OTLP_ENDPOINT` |
| `secret_env` | Read from Secret Manager when the container starts: `{name, version}`, where `name` is the secret id in the engine's project and `version` defaults to `latest` |
| `command`, `args` | Refused. Agent Runtime runs an image's own entrypoint and has no way to change it |

Whatever runs must listen on `0.0.0.0:8080`, which Agent Runtime requires of
every container. Agent Runtime reads a `secret_env` secret as the **Reasoning
Engine Service Agent**, which needs `roles/secretmanager.secretAccessor` on it:

```bash
gcloud secrets add-iam-policy-binding <SECRET> --project=<PROJECT> \
  --member="serviceAccount:service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com" \
  --role=roles/secretmanager.secretAccessor
```

Changing any of these fields changes the plan's config hash, so the next apply
updates the engines.

### Run omnia-dialhome

[omnia-dialhome](https://github.com/AltairaLabs/omnia-dialhome) runs a pack as
the runtime of an agent in an Omnia workspace: the engine opens one outbound
connection to the agent's relay and serves its conversations over it, with
policy, sessions, evals and memory staying in Omnia. It reads the pack and the
agent from the adapter's `PROMPTPACK_PACK_JSON` and `PROMPTPACK_AGENT`.

1. Make the image pullable through the `ghcr-remote` repository from
   [Image](#image): `ghcr.io/altairalabs/omnia-dialhome:<version>`.
2. Put the agent's dial-home key in Secret Manager and grant the service agent
   access to it, as above. Omnia issues the key and the URL together, in the
   agent's `<agent>-dial-home-key` Secret (keys `key` and `url`).

   ```bash
   printf '%s' "$DIAL_HOME_KEY" | gcloud secrets create my-agent-dial-home-key \
     --project=my-project --data-file=-
   ```

3. Deploy with the override:

   ```yaml
   deploy:
     provider: vertex
     config:
       project: my-project
       location: us-central1
       service_account: agent-runtime@my-project.iam.gserviceaccount.com
       providers:
         - { name: default, role: llm, type: gemini, model: gemini-2.5-flash }
       runtime_override:
         image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/omnia-dialhome:v0.1.1
         env:
           OMNIA_DIAL_HOME_URL: wss://my-agent-dial-home.example.com/v1/channel
           OMNIA_DIALHOME_PORT: "8080"
         secret_env:
           OMNIA_DIAL_HOME_KEY: { name: my-agent-dial-home-key }
   ```

`OMNIA_DIALHOME_PORT` makes it answer `/healthz` and `/readyz` on the port
Agent Runtime expects. `providers` is still required by the adapter, but
omnia-dialhome takes its model provider from Omnia over the channel and ignores
`PROMPTPACK_PROVIDERS`.

Two things differ from `vertex-runtime`:

- Before omnia-dialhome v0.6.0, conversations arrive only through Omnia, over
  the channel, and the engine does not serve the
  [runtime contract](#runtime-contract)'s query routes. From v0.6.0 it serves
  `:query` and `:streamQuery` too, running each turn itself and having Omnia
  govern it. The caller puts its identity token in `input.identity_token`,
  beside `input.message` and `input.session_id`; Omnia verifies it against the
  issuers the agent's `AgentRuntime` lists under `spec.frontDoor`. The
  engine's own `Authorization` header is not taken as the caller's identity.
- A pack over `pack_inline_limit_bytes` is staged to Cloud Storage, and
  omnia-dialhome refuses a staged pack. Keep the pack under the limit, or build
  an image `FROM` omnia-dialhome with the pack copied in and set
  `OMNIA_DIALHOME_PACK` in `env`.

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
