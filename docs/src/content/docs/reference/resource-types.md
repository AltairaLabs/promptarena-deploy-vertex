---
title: Resource types
description: Every resource the adapter manages, and which lifecycle operations it supports
---

The adapter surfaces two resource types in plans. There is no gateway, policy,
memory or tool resource: everything else the agent needs is injected into the
engine as container environment.

## `agent_runtime`

One Agent Runtime engine per agent in the pack.

| Property | Value |
|---|---|
| Google resource | `projects/{project}/locations/{location}/reasoningEngines/{id}` |
| API | `aiplatform.googleapis.com`, v1beta1 |
| Plan name | The agent's name |
| Created when | The agent has no engine in prior state |
| Updated when | The pack hash or config hash changed |
| Deleted when | The agent is no longer in the pack |

The engine's display name is the agent's name, and its resource name is stable
across applies — an update replaces the spec in place rather than recreating.

### Where the agent's name comes from

For a multi-agent pack it is the member's name. For a single-prompt pack it is
the prompt's key in the compiled pack, and that key is the prompt's
`task_type` — not the id in `prompt_configs`, and not the arena's
`metadata.name`.

That is worth setting deliberately before the first apply, because the
skeleton most kits start from uses `task_type: general`, which deploys an
engine called `general`:

```console
$ promptarena deploy plan
Plan: 1 to create
  + agent_runtime.general (Create the Agent Runtime engine)
```

Giving the prompt a meaningful `task_type` — and matching it in every scenario
that references it — names the engine after the job it does:

```console
  + agent_runtime.sales_ops (Create the Agent Runtime engine)
```

You can confirm what a pack will produce before deploying anything:

```console
$ promptarena export --config config.arena.yaml --output sample.pack.json
$ python3 -c "import json; print(list(json.load(open('sample.pack.json'))['prompts'].keys()))"
['sales_ops']
```

Renaming later means a new engine: the display name is part of a long-lived
Google resource, so the old one has to be destroyed and a new one applied.

### Spec fields the adapter sets

| Field | Source |
|---|---|
| `displayName` | Agent name |
| `description` | Generated from agent and pack id |
| `containerSpec.imageUri` | `image` |
| `containerSpec.env` | See [environment variables](./environment-variables/) |
| `serviceAccount` | `service_account` |
| `labels` | `labels`, sanitized, plus pack correlation labels |
| `resourceLimits` | `resource_limits` |
| `minInstances` / `maxInstances` | `min_instances` / `max_instances` |
| `containerConcurrency` | `container_concurrency` |

`ContainerSpec` lives in a protobuf `oneof` (`DeploymentSource`), which is why
the adapter builds the whole deployment source rather than patching fields.

An agent card is generated but **not attached** — `spec.agentCard` exists in the
v1beta1 REST discovery document but not in the published protos, so the Go
client has no field to set.

### States

| State | Meaning |
|---|---|
| `ACTIVE` | Serving |
| `CREATING` | Create in progress |
| Recorded as in-flight | Creation did not complete; reconciled on the next apply |

## `pack_object`

The compiled pack, staged to Cloud Storage. Appears only when the serialized
pack exceeds `pack_inline_limit_bytes` (24576 by default).

| Property | Value |
|---|---|
| Google resource | An object in `staging_bucket` |
| Plan name | `pack.json` |
| Created when | The pack exceeds the inline limit |

The engine then receives `PROMPTPACK_PACK_URI` instead of
`PROMPTPACK_PACK_JSON`.

A pack over the limit with no `staging_bucket` fails at plan time rather than
at deploy time.

## Lifecycle support

| Operation | `agent_runtime` | `pack_object` |
|---|---|---|
| Create | Yes | Yes |
| Update | Yes, in place | Replaced |
| Delete (agent left pack) | Yes | — |
| `destroy` | Yes | Not implemented |
| `status` | Yes | Not implemented |
| `import` | Not implemented | Not implemented |

`destroy` deletes the engines. It does **not** delete a pack staged to Cloud
Storage: the object is small and harmless, but it is left behind, so a bucket
used for many deploys accumulates them. Remove them with `gcloud storage rm` if
that matters to you.

`status` reports engine health only, for the same reason — a staged object has
no health to report beyond existing.
