---
title: Runtime protocols
description: The HTTP contract the vertex-runtime container serves
---

`vertex-runtime` listens on `0.0.0.0:8080` — fixed by the Agent Runtime
contract, not configurable — and serves two routes.

| Route | Purpose | Response |
|---|---|---|
| `POST /api/reasoning_engine` | Unary turn | `{"output":"…"}` |
| `POST /api/stream_reasoning_engine` | Streaming turn | ndjson, one `{"output":"…"}` per chunk |

Callers reach these through the Agent Runtime API as `:query` and
`:streamQuery` on the engine resource.

## Request

```json
{
  "class_method": "query",
  "input": { "message": "hello" }
}
```

| Field | Notes |
|---|---|
| `class_method` | Defaults to `query` / `stream_query` per route when omitted. |
| `input.message` | The user turn. `input`, `prompt` also accepted. |

When more than one input key is present, `message` wins, then `input`, then
`prompt`. A missing or non-string message is a request error.

## Unary response

```json
{ "output": "Hello — how can I help?" }
```

## Streaming response

Newline-delimited JSON, one line per text chunk, flushed after each:

```
{"output":"Hello"}
{"output":" — how"}
{"output":" can I help?"}
```

This is real incremental output, not one buffered response split at the end.

### Errors after streaming begins

If the turn fails once streaming has started, the failure arrives as a trailing
line:

```
{"output":"Hello"}
{"error":"provider error: …"}
```

The HTTP status is already `200` by then and cannot be retracted, so a streaming
client must inspect each line rather than trusting the status code alone.

## Calling it

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/<RESOURCE_NAME>:query" \
  -d '{"class_method":"query","input":{"message":"hello"}}'
```

Streaming uses the same shape against `:streamQuery` with `class_method` set to
`stream_query`.

## Conversation state

Each request opens a fresh conversation. The runtime holds no session state
between calls, so multi-turn context is the caller's responsibility — send the
history you want the model to see.
