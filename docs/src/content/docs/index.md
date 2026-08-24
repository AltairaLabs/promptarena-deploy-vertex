---
title: Google Agent Runtime Adapter
description: Deploy prompt packs to Google Agent Runtime (formerly Vertex AI Agent Engine)
sidebar:
  order: 0
---

**Deploy prompt packs to Google Agent Runtime as managed container engines.**

---

## What is the Vertex adapter?

The Vertex adapter is a deploy provider plugin for PromptKit. It turns your
compiled `.pack.json` into **Agent Runtime engines** — one per agent in the pack —
each running the `vertex-runtime` container with your pack, provider bindings and
tool execution config injected as environment.

Agent Runtime was formerly Vertex AI Agent Engine. The API resource is still
`reasoningEngines` and the API host is still `aiplatform.googleapis.com`, which is
why the adapter is named `vertex`.

### What it creates

| Pack concept | Google resource | Adapter resource type |
|---|---|---|
| Agent prompt | Agent Runtime engine (`reasoningEngines`) | `agent_runtime` |
| Pack too large to inline | Cloud Storage object | `pack_object` |
| Pack tools | Injected execution config (no separate resource) | *(env var injection)* |
| Provider bindings | Injected config, resolved against the arena | *(env var injection)* |

Unlike the AgentCore adapter, there is no gateway, policy engine or memory
resource to create: everything the agent needs travels inside the container.

### What makes it different

- **It is PromptKit all the way down.** The container runs the PromptKit SDK, not
  a Google agent framework. The same pack behaves the same locally and deployed.
- **Real streaming.** The runtime serves the Agent Runtime ndjson contract with
  one line per text chunk, not a single buffered response at the end.
- **ADC only.** No API keys. Inside Agent Runtime the identity is the
  deployment's service account.
- **One engine per agent.** A multi-agent pack deploys as independent engines
  with no routing between them — see [A2A](explanation/resource-lifecycle).

---

## Quick start

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

```bash
promptarena deploy plan
promptarena deploy apply
```

Start with [your first deployment](tutorials/01-first-deployment), which
covers the two IAM grants that fail *after* the engine is created rather than at
validation time.

---

## Adapter status

| Method | Status |
|---|---|
| `get_provider_info` | Implemented |
| `validate_config` | Implemented |
| `plan` | Implemented |
| `apply` | Implemented |
| `destroy` | Implemented |
| `status` | Implemented |
| `import` | Not implemented |

`import` is not implemented; neither sibling adapter implements it either, so
an engine created outside promptarena cannot be adopted into state.

## Known gaps

These are documented rather than hidden, because each one fails at a point where
the deployment still looks healthy:

- **`cloudbuild` image mode is refused.** Nothing builds the image, so it is
  rejected at plan rather than deploying an engine with no image. Use
  `prebuilt`.
- **A2A is not wired.** Agent cards are generated but cannot be attached.
- **Guardrails are invisible in telemetry.** Evals are traced; a firing
  `validators` entry leaves no trace attribute.
