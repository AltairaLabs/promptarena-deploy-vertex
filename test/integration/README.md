# Deployed integration tests

These deploy a real pack to Agent Runtime, invoke the engine, and delete it.
They are the only tests that prove the adapter works against Google's platform
rather than against our own assumptions.

They are **not** run in CI: they need GCP credentials and cost money. Run them by
hand before a release, or after any change to the runtime contract, the engine
spec, or the client.

## Running

```bash
export VERTEX_TEST_PROJECT=my-project
export VERTEX_TEST_LOCATION=us-central1
export VERTEX_TEST_IMAGE=us-central1-docker.pkg.dev/my-project/promptkit/vertex-runtime:test
make test-integration
```

Without `VERTEX_TEST_PROJECT` and `VERTEX_TEST_IMAGE` every test skips, so a
normal `go test ./...` can never create resources.

## Prerequisites

**The image must be in Artifact Registry.** Agent Runtime cannot pull from
`ghcr.io` directly — use an AR remote repository as a pull-through cache, or push
to AR directly.

**The Reasoning Engine Service Agent must be able to read it:**

```bash
gcloud artifacts repositories add-iam-policy-binding <REPO> \
  --location=<LOCATION> --project=<PROJECT> \
  --member="serviceAccount:service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com" \
  --role=roles/artifactregistry.reader
```

Without it the engine is **created and then fails to start** — the create call
itself succeeds, so nothing catches it earlier.

## Cost and cleanup

Each test deploys its own engine and deletes it via `t.Cleanup`, including when
the test fails. `min_instances` defaults to 1, so an engine that survives a crash
**bills continuously**. If a run is interrupted, check for leftovers:

```bash
curl -H "Authorization: Bearer $(gcloud auth print-access-token)" \
  "https://us-central1-aiplatform.googleapis.com/v1beta1/projects/<PROJECT>/locations/<LOCATION>/reasoningEngines"
```

A failed cleanup is reported as a test error naming the engine to delete.

## What these cover, and what they do not

Covered: unary invocation, streaming invocation, multi-turn, and idempotent
re-apply — all against a pack declaring a tool, a validator and a template
variable.

**Not covered**, and not implied by a green run: tool *execution* (the model must
choose to call one, which is not asserted), Agent Gateway egress, non-Gemini
providers, Model Garden, A2A agent cards, and multi-agent topology.

## Note on running the package directly

`go test ./test/integration/` without the tag reports
`build constraints exclude all Go files` and exits non-zero. That is the guard
working. Use `make test-integration`, or include the package via `./...`, which
skips it cleanly.
