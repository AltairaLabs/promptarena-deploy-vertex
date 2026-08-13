---
title: Security model
description: Identities, IAM grants, and where your pack and secrets end up
---

Three identities are involved in a deployment, and confusing them is the most
common cause of a deployment that creates cleanly and then fails.

## The three identities

| Identity | Used for | Needs |
|---|---|---|
| **You** (ADC) | Running `plan` and `apply` | `roles/aiplatform.admin` or equivalent create/update on `reasoningEngines` |
| **Reasoning Engine Service Agent** | Pulling the container image | `roles/artifactregistry.reader` on the image's repository |
| **The engine's service account** | Calling models at runtime | `roles/aiplatform.user` |

The Reasoning Engine Service Agent is Google-managed and named
`service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com`. It
appears once the AI Platform API is enabled.

:::caution[The image grant fails after creation, not during it]
Without `roles/artifactregistry.reader`, `apply` succeeds and the engine then
fails to start with an image access error. The adapter warns about this at plan
time because there is no way for it to check ahead — checking would require
permissions the adapter does not ask for.
:::

## Application Default Credentials

Authentication is ADC end to end. There are no API keys anywhere in this
adapter, and no provider credentials are injected into the container.

Inside Agent Runtime, ADC resolves to the engine's service account. Locally, it
resolves to whatever `gcloud auth application-default login` last set — which is
**not** the same as the account in `gcloud auth list`. When the CLI works and
the adapter gets 403, that difference is almost always the cause.

If `service_account` is unset, the engine runs as the Reasoning Engine Service
Agent, which usually lacks `roles/aiplatform.user`. Set it explicitly and grant
that role.

## Where your pack ends up

The compiled pack is injected as a container environment variable, or — above
the inline limit — staged as a Cloud Storage object and fetched at startup.

Both are readable by anyone who can describe the engine or read the bucket.
**A prompt pack is not a secret store.** Anything sensitive in a system template
is visible to any principal with `reasoningEngines.get`.

Staged pack objects live in the bucket you configure. The adapter does not set
bucket-level access controls; apply your own, and prefer a bucket that is not
also serving something public.

## Tool calls leave the perimeter

A `live` tool calls its URL from inside the engine. That request carries no GCP
credentials and no ADC token — the endpoint sees an ordinary HTTP call from
Google's network.

Headers are not forwarded yet, so there is currently no way to authenticate a
live tool call from a deployed engine. Treat live tools as reaching public,
unauthenticated endpoints only.

## Labels are not access control

Labels are sanitized to GCP's rules and used for correlation. Two keys that
would sanitize to the same label are rejected rather than merged — a silent
merge would drop one, and a dropped label on a resource used for cost or
ownership attribution is a real problem.

They carry no security meaning: anyone who can list engines can read them.

## What the adapter never does

- It does not create or modify IAM policy. Every grant above is yours to make.
- It does not read arena provider secrets. Bindings resolve to a type and model
  only; credentials come from ADC at runtime.
- It does not enable APIs on your behalf.
