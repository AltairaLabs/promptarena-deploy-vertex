---
name: Bug Report
about: Report a bug in the Vertex AI Agent Runtime deploy adapter
title: "[BUG] "
labels: bug, needs-triage
assignees: ""
---

## Bug Description

A clear and concise description of the bug.

## Steps to Reproduce

1. Configure the adapter with ...
2. Run `promptarena deploy plan` / `promptarena deploy apply`
3. Observe ...

## Expected Behavior

A clear and concise description of what you expected to happen.

## Actual Behavior

A clear and concise description of what actually happened.

## Environment

- **Adapter version**: (e.g., v0.1.0 — `promptarena deploy adapter install vertex` installs the release build)
- **Runtime image**: (e.g., ghcr.io/altairalabs/promptkit-vertex-runtime:v0.1.0, or the Artifact Registry path you actually deployed)
- **OS**: (e.g., macOS 15.3, Ubuntu 24.04)
- **Go version**: (e.g., go1.26.0)
- **Vertex location**: (e.g., us-central1)
- **Project**: a project ID or project number often identifies an organisation — describe it instead if that matters to you

## Configuration

Relevant deploy config (redact any secrets):

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    image: us-central1-docker.pkg.dev/my-project/ghcr-remote/altairalabs/promptkit-vertex-runtime
    providers:
      - name: default
        role: llm
        arena_provider: gemini-flash
    # staging_bucket: gs://my-bucket   # needed once a pack exceeds the inline limit
```

## Error Output

```
Paste any error messages, logs, or stack traces here.
```

## Before you file

A few things are known gaps rather than bugs — please open a feature request instead if
your report is one of these:

- `destroy`, `status` and `import` are not implemented; teardown is manual.
- `image_mode: cloudbuild` is config surface only — `apply` always deploys `image`.
- Tools: `live` supports `url` and `method` only; `mcp`, `exec` and `client` are unsupported.
- Provider bindings with a role other than `llm` round-trip through config without effect.
- Agent cards are generated but not attached (no field for it in the v1beta1 Go client).

Two failure modes that look like adapter bugs but are usually project setup:

- **Engine creates, then fails to start with an image access error** — the Reasoning Engine
  Service Agent (`service-<PROJECT_NUMBER>@gcp-sa-aiplatform-re.iam.gserviceaccount.com`)
  needs `roles/artifactregistry.reader`.
- **Image cannot be pulled from `ghcr.io`** — Agent Runtime cannot pull from ghcr.io. Use an
  Artifact Registry remote (pull-through) repository, or push the image to AR directly.

## Additional Context

Add any other context about the problem here (screenshots, related issues, etc.).

## Documentation Impact

**Would closing this make something in the docs untrue?**
Check the docs for statements this issue would invalidate — a documented limitation,
a "not supported yet", or a described behaviour that changes. Docs deliberately carry
no issue links, so nothing points back here when this closes; the doc update has to be
part of the work.

This repo's `docs/` is republished into the PromptArena documentation site, so a fix
made there reverts on the next fetch — this repo is the only place it sticks.

- [ ] No documented statement changes
- [ ] Yes — the pages to update are listed below, and updating them is part of this issue

```
<paths under docs/src/content/docs/ that this makes stale>
```

## Checklist

- [ ] I have searched existing issues to ensure this is not a duplicate.
- [ ] I have included all relevant environment details.
- [ ] I have provided a minimal configuration to reproduce the issue.
- [ ] I have redacted any secrets or sensitive information from the config and logs.
- [ ] I have checked whether fixing this makes any existing documentation untrue, and listed the pages if so
