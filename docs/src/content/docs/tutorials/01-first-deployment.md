---
title: Your first deployment
description: Deploy a single-agent prompt pack to Google Agent Runtime end to end
---

By the end of this tutorial a real Agent Runtime engine will answer a real
question using a real Gemini model.

Two of the steps below exist because Agent Runtime fails **after** the engine is
created rather than at validation time. Skipping them produces an engine that
exists, reports no config error, and never starts.

## Prerequisites

- A GCP project with billing enabled
- `gcloud` authenticated, and Application Default Credentials set up:
  ```bash
  gcloud auth login
  gcloud auth application-default login
  gcloud config set project my-project
  ```
- The adapter installed. There is no published release yet, so build it from
  source and drop the binary where the CLI looks for it:
  ```bash
  git clone https://github.com/AltairaLabs/promptarena-deploy-vertex
  cd promptarena-deploy-vertex && make build-adapter
  mkdir -p ~/.promptarena/adapters
  cp promptarena-deploy-vertex ~/.promptarena/adapters/
  chmod 755 ~/.promptarena/adapters/promptarena-deploy-vertex
  ```
  Once the adapter is released, `promptarena deploy adapter install vertex` will
  do this for you.

:::caution[ADC is not the same as the `gcloud` CLI account]
Terraform and the Google SDKs read Application Default Credentials, which drift
independently from `gcloud auth list`. If the CLI works and the adapter gets 403,
ADC is almost always the reason.
:::

## Step 1: Enable the APIs

```bash
gcloud services enable \
  aiplatform.googleapis.com \
  artifactregistry.googleapis.com \
  --project=my-project
```

## Step 2: Get the runtime image into Artifact Registry

Agent Runtime **cannot pull from `ghcr.io`**. Create an Artifact Registry remote
repository as a pull-through cache:

```bash
gcloud artifacts repositories create ghcr-remote \
  --repository-format=docker \
  --mode=remote-repository \
  --location=us-central1 \
  --remote-docker-repo=https://ghcr.io \
  --project=my-project
```

The image is then addressable as:

```
us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
```

This assumes the GHCR package is public. For a private package, push the image
to Artifact Registry directly — upstream credentials on a remote repository are
not supported yet.

## Step 3: Grant the Reasoning Engine Service Agent image access

This is the grant that fails late. Without it, `apply` succeeds and the engine
then fails to start with an image access error.

```bash
gcloud projects describe my-project --format='value(projectNumber)'
```

Using that number:

```bash
gcloud artifacts repositories add-iam-policy-binding ghcr-remote \
  --location=us-central1 \
  --project=my-project \
  --member="serviceAccount:service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader
```

## Step 4: Create a service account for the engine

```bash
gcloud iam service-accounts create agent --project=my-project

gcloud projects add-iam-policy-binding my-project \
  --member="serviceAccount:agent@my-project.iam.gserviceaccount.com" \
  --role=roles/aiplatform.user
```

If you leave `service_account` unset, the engine runs as the Reasoning Engine
Service Agent instead, which usually lacks `roles/aiplatform.user` — the adapter
warns about this at plan time.

## Step 5: Configure the deploy target

In your arena config:

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    service_account: agent@my-project.iam.gserviceaccount.com
    providers:
      - name: default
        role: llm
        type: gemini
        model: gemini-2.5-flash
```

`providers` is required. The binding named `default` is the primary LLM.

## Step 6: Plan

```bash
promptarena deploy plan
```

`plan` performs **no GCP calls**. It diffs the pack and config hashes against
prior adapter state and reports what an apply would change:

```
+ agent_runtime  support   create
```

Read the warnings. They cover the two IAM grants above, plus anything unusual
about the pack.

## Step 7: Apply

```bash
promptarena deploy apply
```

Creating an engine takes a few minutes. The adapter records the resource name in
state; if creation does not finish, the engine is recorded as in-flight and
reconciled on the next apply rather than orphaned.

## Step 8: Query it

Take the resource name from the apply output, then:

```bash
curl -X POST \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  -H "Content-Type: application/json" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/<RESOURCE_NAME>:query" \
  -d '{"class_method":"query","input":{"message":"hello"}}'
```

```json
{ "output": "Hello — how can I help?" }
```

For token-by-token output, use `:streamQuery` with `class_method`
`stream_query`; the response is ndjson, one `{"output":"…"}` line per chunk.

## Step 9: Clean up

`destroy` is not implemented yet, so delete the engine yourself:

```bash
curl -X DELETE \
  -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/<RESOURCE_NAME>"
```

An engine left running with `min_instances` above zero keeps billing.

## What next

- [Configure the adapter](/reference/configuration/) — every option
- [Give your agent working tools](/how-to/tools/) — the step most people miss
- [Turn on tracing](/how-to/observability/)
