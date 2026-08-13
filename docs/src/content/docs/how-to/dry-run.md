---
title: Preview a deployment with dry run
description: Plan and apply without calling Google APIs
---

Dry run exercises the whole adapter — config validation, provider resolution,
pack delivery, engine specs, state — without creating anything in GCP.

## Enable it

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    dry_run: true
    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
```

```bash
promptarena deploy apply
```

The adapter swaps its real GCP client for one that records intent and returns
synthetic resource names. No engine is created, nothing is billed, and no
credentials are needed.

## What it does and does not check

Dry run catches everything that is decidable locally:

- Missing or malformed config fields
- Provider bindings that reference an arena provider that does not exist
- Labels that violate GCP's rules or collide after sanitization
- Packs that exceed the inline limit with no staging bucket
- Scaling bounds that contradict each other

It cannot catch anything that depends on the state of your project:

- Whether the image exists in Artifact Registry
- Whether the Reasoning Engine Service Agent can read it
- Whether the service account holds `roles/aiplatform.user`
- Whether the API is enabled, or the quota is available

Those are exactly the failures that surface **after** the engine is created. A
green dry run is not a promise that apply will work — it is a promise that the
adapter has nothing left to complain about.

## `plan` is already cheap

`plan` never calls GCP either, dry run or not. It diffs the pack hash, the
config hash and prior state, and reports the resource changes an apply would
make. Reach for `dry_run` when you want to exercise `apply`'s code path —
in CI, for example — not merely to see a diff.

## State

A dry-run apply writes state containing synthetic resource names. Do not carry
that state into a real apply: the adapter would treat the fake engines as
existing and try to update resources that were never created. Keep dry-run runs
in their own state file.
