---
title: Environment variables
description: Every variable injected into the runtime container, and where its value comes from
---

The adapter configures the deployed engine entirely through container
environment. This is the full set.

| Variable | Source | Condition |
|---|---|---|
| `PROMPTPACK_PACK_JSON` | Compiled pack | Pack at or below the inline limit |
| `PROMPTPACK_PACK_URI` | Staged pack object | Pack above the inline limit |
| `PROMPTPACK_AGENT` | Agent name | Always |
| `PROMPTPACK_PROVIDERS` | Resolved provider bindings | Always |
| `PROMPTPACK_PROJECT` | `project` | Always |
| `PROMPTPACK_LOCATION` | `location` | Always |
| `PROMPTPACK_TOOL_SPECS` | Arena `tool_specs` | Arena declares tool specs |
| `PROMPTPACK_TRACING_ENABLED` | `observability.tracing_enabled` | Tracing enabled |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `observability.otlp_endpoint` | Tracing enabled |

Exactly one of `PROMPTPACK_PACK_JSON` or `PROMPTPACK_PACK_URI` is set; the
runtime requires one of them and fails to start without either.

## Why the GCP coordinates are prefixed

Agent Runtime **reserves** `GOOGLE_CLOUD_PROJECT` and `GOOGLE_CLOUD_LOCATION`
and rejects a deployment that sets them:

```
Environment variable name 'GOOGLE_CLOUD_PROJECT' is reserved
```

It reserves them because it injects them itself. The adapter therefore sets
`PROMPTPACK_PROJECT` and `PROMPTPACK_LOCATION`, and the runtime reads the
prefixed names first, falling back to the conventional ones.

The result is one image that runs unchanged on Agent Runtime (where Google
injects the coordinates) and on Cloud Run, a local shell or CI (where nothing
does).

## `PROMPTPACK_PROVIDERS`

A JSON list of resolved bindings. The binding named `default` is the primary
LLM; otherwise the first with role `llm` is used.

```json
[{"name":"default","role":"llm","type":"gemini","model":"gemini-2.5-flash"}]
```

Bindings resolve at plan time — `arena_provider` references are expanded into
`type` and `model` before injection, so the container never reads the arena
config.

No credentials appear here. Authentication is Application Default Credentials,
which inside Agent Runtime is the deployment's service account.

## `PROMPTPACK_TOOL_SPECS`

A JSON map of tool name to execution config, forwarded verbatim from the
arena's `tool_specs`:

```json
{
  "lookup_order": {
    "name": "lookup_order",
    "mode": "mock",
    "mock_template": "{\"order_id\":\"{{.order_id}}\",\"status\":\"shipped\"}"
  }
}
```

Absent when the arena declares no tool specs — in which case the pack's tools
are advertised to the model with nothing able to run them. See
[giving your agent working tools](/how-to/tools/).

## `OTEL_EXPORTER_OTLP_ENDPOINT`

The standard OpenTelemetry variable, used as-is rather than a name invented for
this adapter, so the image works with any OTLP collector. Must be a full URL
including the scheme.

## Local use

The same variables drive the runtime outside GCP:

```bash
PROMPTPACK_PACK_JSON="$(cat some.pack.json)" \
PROMPTPACK_PROVIDERS='[{"name":"default","role":"llm","type":"gemini","model":"gemini-2.5-flash"}]' \
GOOGLE_CLOUD_PROJECT=my-project \
GOOGLE_CLOUD_LOCATION=us-central1 \
  ./vertex-runtime
```
