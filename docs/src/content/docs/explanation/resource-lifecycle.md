---
title: Resource lifecycle
description: How the adapter decides what changed, and how engines are created, updated and removed
---

The adapter manages one resource type in the common case: an **Agent Runtime
engine** per agent in the pack. Everything else the agent needs — the pack, the
provider bindings, the tool execution config — travels inside that engine as
container environment.

That design decision explains most of the lifecycle behavior below. There is no
separate tool resource to drift, and no policy resource to reconcile, but it
also means *any* change to the pack or config is a change to every engine.

## What `plan` compares

`plan` performs **no GCP calls**. It diffs three inputs against prior adapter
state:

| Input | Effect |
|---|---|
| Pack hash | Any pack change updates every engine |
| Config hash | Project, location, image, service account, scaling, labels, resolved provider bindings, observability, tool specs |
| Prior state | Engines absent from state are created; engines whose agent left the pack are deleted |

The pack is hashed as the CLI handed it over — the bytes as received, not
re-serialized. Re-encoding could reorder map keys and produce a diff for a pack
that did not change.

`dry_run` is deliberately **excluded** from the config hash. It changes adapter
behavior, not deployed state, so toggling it must not read as a change.

## Create

`apply` creates one engine per agent, using the display name to correlate it
back to the agent. Creation takes minutes, and the adapter records the resource
name in state as soon as it has one.

If creation does not finish — the process is interrupted, the operation times
out — the engine is recorded as **in-flight**. The next apply reconciles it
rather than leaving it orphaned and invisible. This matters because an orphaned
engine still bills.

## Update

An engine whose agent is unchanged is updated **in place**. The resource name is
stable across applies, so re-applying an unchanged pack is a genuine no-op, and
consumers holding a resource name keep working across deploys.

Because the whole configuration is container environment, an update is an
environment replacement. There is no partial update: change one label and the
engine's full desired spec is re-sent.

## Delete

An engine present in state but whose agent has left the pack is deleted on the
next apply.

`destroy` deletes every engine in state. An engine that is already gone counts
as success, so a teardown retried after a partial failure converges rather than
becoming manual work, and one engine failing to delete does not strand the
rest — each is attempted and every failure is reported.

An engine recorded mid-creation has no resource name to address. Destroy reports
it rather than skipping it silently, because it may still be running and only
you can resolve that.

`import` is not implemented, so an engine the adapter did not create cannot be
adopted into state.

## Pack delivery

Packs at or below `pack_inline_limit_bytes` (24576 by default) ride in an
environment variable. Larger packs are staged to Cloud Storage and appear in the
plan as a `pack_object` resource; the engine then receives a `gs://` URI instead
of the pack itself.

The limit exists because the Cloud Run substrate caps total environment size
near 32 KiB, and the pack is not the only variable being injected. A pack that
exceeds the limit with no `staging_bucket` configured fails at plan time rather
than at deploy time.

The staged object is keyed by pack hash — `packs/<hash>/pack.json` — so
re-applying an unchanged pack uploads nothing, and a changed pack lands beside
the old one rather than overwriting it. That matters because engines resolve
their pack at startup: overwriting in place would hand a restarting engine the
new pack while its siblings still ran the old one. The trade is that old
objects accumulate, and `destroy` does not remove them.

## Multi-agent packs

Every agent becomes an independent engine sharing the same image, service
account, scaling and provider bindings. Per-agent overrides are not supported.

`plan` warns when a pack will deploy this way, because "several engines" and
"a multi-agent system" are not the same thing: nothing routes between them.

## A2A is not wired yet

The adapter generates an agent card for each agent, but cannot attach it.
`ReasoningEngineSpec.agentCard` exists in the v1beta1 REST discovery document
and is absent from the published protobuf definitions, so the Go client has no
field to set.

`plan` warns when a pack declares `a2a__` tools. Those tool calls will not
reach another agent — they will be advertised to the model and go unfulfilled,
the same failure mode as a tool with no execution config.

Setting `spec.agentCard` so A2A discovery works is therefore not yet supported.
