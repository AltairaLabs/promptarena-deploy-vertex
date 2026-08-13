---
title: Give your agent working tools
description: Why a deployed agent's tools do nothing without arena tool_specs, and how to fix it
---

A deployed agent can advertise tools to the model and still have nothing able to
run them. This guide explains why, and what to configure so tool calls actually
execute.

## The problem

A compiled `.pack.json` carries a tool's **schema** — name, description,
parameters — but not how to run it. Execution config lives in the arena config
under `tool_specs`.

If that config never reaches the adapter, the deployed engine registers the
tool's schema, the model dutifully calls it, nothing answers, and the model
apologizes:

```
I'm sorry, but the lookup_order tool is not working right now.
```

The deployment looks healthy. The engine is running. Only the answer is wrong.

## The fix

Define `tool_specs` in the arena config that you deploy from. The CLI passes it
to the adapter, which forwards it to the engine as `PROMPTPACK_TOOL_SPECS`.

```yaml
tool_specs:
  lookup_order:
    name: lookup_order
    mode: mock
    mock_template: |
      {"order_id":"{{.order_id}}","status":"shipped"}
```

Re-plan and the change shows up:

```
~ agent_runtime  support   update
```

Tool specs are part of the config hash, so editing a `mock_result` is a real
diff — not a silent no-op that leaves the old value deployed.

## Supported modes

| Mode | Support |
|---|---|
| `mock` | Full — `mock_result` and `mock_template`. |
| `live` | HTTP `url` and `method` only. |
| `mcp` | Not supported. |
| `exec` | Not supported. |
| `client` | Not supported. |

`mcp`, `exec` and `client` each need something the container does not have — an
MCP server, a subprocess, or a client on the other end of the connection.

Tools in an unsupported mode are **logged once at startup and left
unregistered**, rather than failing the deployment. The rest of the agent still
works; check the engine's logs if a tool never fires.

### `mock`

```yaml
tool_specs:
  # Static result — same answer regardless of arguments
  get_policy:
    mode: mock
    mock_result:
      refund_window_days: 30

  # Templated result — depends on the call arguments
  lookup_order:
    mode: mock
    mock_template: |
      {"order_id":"{{.order_id}}","status":"shipped"}
```

`mock_template` wins when both are set.

Template semantics match `promptarena run` exactly: missing keys render as the
zero value, and the rendered text is parsed back as JSON. Text that is not valid
JSON is wrapped as `{"result": "<text>"}`.

That parity is the point. A deployed agent that answered differently from a
local run would make your local evals meaningless.

### `live`

```yaml
tool_specs:
  get_weather:
    mode: live
    http:
      url: https://api.example.com/weather
      method: GET
```

Only `url` and `method` are forwarded. Headers, timeouts, redaction and
request/response mapping are **not** carried yet, so a live tool needing an auth
header will not work. A `live` tool with no `http.url` is reported as
unsupported rather than registered.

The engine calls the URL from inside Agent Runtime, so the endpoint must be
reachable from Google's network and the engine's service account identity is
irrelevant to it — the call carries no GCP credentials.

## Verifying it works

Ask the model something only the tool can answer, with a value that appears
nowhere in the prompt:

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/<RESOURCE_NAME>:query" \
  -d '{"class_method":"query","input":{"message":"Use lookup_order for order 42 and tell me what it says."}}'
```

```json
{ "output": "Order 42 has shipped." }
```

If the answer is an apology, the tool did not run. Check, in order:

1. Does the deploy config's arena actually declare `tool_specs`?
2. Does the engine have `PROMPTPACK_TOOL_SPECS` in its environment?
3. Do the engine's logs report the tool as an unsupported mode?

:::tip[Assert on the tool's data, not on "a response came back"]
A test that asserts only "output is non-empty" passes on the apology. Assert a
string that only the tool could have supplied.
:::
