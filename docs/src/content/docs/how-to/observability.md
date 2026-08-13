---
title: Turn on tracing
description: OTLP spans, eval scores, and what the deployed engine does not report
---

Tracing is **off by default** — an unconfigured deployment sends nothing and
pays nothing.

## Enable it

```yaml
deploy:
  provider: vertex
  config:
    observability:
      tracing_enabled: true
      otlp_endpoint: http://collector.observability.svc:4318
```

This injects `PROMPTPACK_TRACING_ENABLED` and the standard
`OTEL_EXPORTER_OTLP_ENDPOINT` into the container, so the same image works with
any OTLP collector.

:::danger[`otlp_endpoint` must include the scheme]
The exporter builds its target with `otlptracehttp.WithEndpointURL`. A
`host:port` value produces `http:///v1/traces` — no host — and every export
fails while the deployment looks perfectly healthy. Validation rejects it, which
is the only reason you find out.
:::

Enabling tracing without an endpoint is also rejected: deploying silently
untraced is worse than refusing the config.

## What you get

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

## Evals are traced; guardrails are not

This distinction is easy to trip over and worth internalizing.

An **eval** — declared in the pack's `evals` section — runs through the eval
runner, which emits an eval-completed event. The telemetry listener turns that
into the span attributes above.

A **guardrail** — a `validators` entry on a prompt — computes using the same
handlers but runs through the guardrail hook adapter, which emits **no event**.
A firing guardrail rewrites the response and leaves no trace attribute behind.
The listener even has a `promptkit.guardrail` flag for the distinction; nothing
currently sets it true.

So a pack with only `validators` produces provider spans but no evaluation
scores. To see scores, declare an `evals` section:

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

A guardrail that fires does write a log line in the engine's logs, so it is not
completely invisible — it is invisible *in traces and metrics*, which is where
you would look for it.

## Not verified

Whether Google Cloud Trace accepts OTLP directly from an Agent Runtime
container, and on what host and with which credentials, is **untested**. Point
`otlp_endpoint` at a collector you control and export onwards from there.

No Cloud Monitoring metrics are emitted. Traces are the only signal today.
