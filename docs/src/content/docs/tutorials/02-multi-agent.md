---
title: Deploying a multi-agent pack
description: One Agent Runtime engine per agent, and what the adapter does not wire for you
---

A pack with several prompts deploys as several **independent** Agent Runtime
engines — one per agent. This tutorial deploys one and shows exactly where the
independence bites.

:::caution[There is no routing between the engines]
The adapter does not create a supervisor, a router, or any agent-to-agent link.
Each engine answers only what you send it directly. If your pack expects one
agent to call another, that call will not happen — see
[A2A is not wired yet](/explanation/resource-lifecycle/).
:::

## The pack

Two prompts, so two engines:

```json
{
  "id": "support-desk",
  "prompts": {
    "triage": {
      "id": "triage",
      "system_template": "Classify the request as billing, technical, or other. Answer with one word."
    },
    "billing": {
      "id": "billing",
      "system_template": "You are a billing specialist. Be precise about amounts and dates."
    }
  }
}
```

## Deploy config

Nothing changes for multi-agent — the same block deploys every agent in the pack:

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
        type: gemini
        model: gemini-2.5-flash
```

Every engine gets the **same** image, service account, scaling and provider
bindings. Per-agent overrides are not supported.

## Plan

```bash
promptarena deploy plan
```

```
+ agent_runtime  triage    create
+ agent_runtime  billing   create
```

The plan also warns that a multi-agent pack deploys as independent engines with
no routing between them. That warning is not noise — read it once per pack.

## Apply

```bash
promptarena deploy apply
```

Each engine takes a few minutes. Adapter state records one entry per agent,
keyed by agent name, so a later apply matches them up rather than recreating.

## Query each engine directly

Each agent has its own resource name. `PROMPTPACK_AGENT` inside the container
selects which prompt that engine serves, so the same image runs both:

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/<TRIAGE_RESOURCE_NAME>:query" \
  -d '{"class_method":"query","input":{"message":"my invoice is wrong"}}'
```

```json
{ "output": "billing" }
```

Routing that answer to the billing engine is your application's job. The
adapter deployed both engines; it did not connect them.

## What changes when an agent leaves the pack

Remove `billing` from the pack and re-plan:

```
- agent_runtime  billing   delete
```

Engines present in adapter state but absent from the pack are deleted on the
next apply. Engines whose agent is unchanged are updated in place — the resource
name is stable across applies, so re-applying an unchanged pack is a no-op.

## Cost

Every agent is a separate engine, and every engine with `min_instances` above
zero bills continuously. A ten-prompt pack is ten engines. Leave `min_instances`
unset while iterating.

## What next

- [Give your agents working tools](/how-to/tools/)
- [Preview changes without deploying](/how-to/dry-run/)
- [How the adapter decides what changed](/explanation/resource-lifecycle/)
