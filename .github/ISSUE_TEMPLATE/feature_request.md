---
name: Feature Request
about: Suggest a new feature or improvement for the Vertex AI Agent Runtime deploy adapter
title: "[FEATURE] "
labels: enhancement, needs-triage
assignees: ""
---

## Feature Summary

A brief, one-line summary of the feature.

## Problem Statement

Describe the problem or limitation you are experiencing. Why is this feature needed?

## Proposed Solution

Describe your proposed solution in detail.

### Config Example

```yaml
deploy:
  provider: vertex
  config:
    project: my-project
    location: us-central1
    # proposed new configuration options
```

Config is validated against a published JSON Schema with `additionalProperties: false`
at both the top level and inside provider bindings, so a new option means a schema
change as well as a struct field.

## Alternative Solutions

Describe any alternative solutions or workarounds you have considered.

## Use Cases

- **Use case 1**: ...
- **Use case 2**: ...

## Implementation Considerations

- Are there any breaking changes to existing deploy configurations?
- Does this need a change to the runtime container as well as the adapter? The two ship
  separately — the adapter is a plugin binary, the runtime is
  `ghcr.io/altairalabs/promptkit-vertex-runtime`, built linux/amd64.
- Does it fit the Agent Runtime contract? The container must listen on `0.0.0.0:8080` and
  serve `/api/reasoning_engine` and `/api/stream_reasoning_engine`; none of that is
  configurable.
- Does it need environment variables? Agent Runtime reserves `GOOGLE_CLOUD_PROJECT` and
  `GOOGLE_CLOUD_LOCATION` and rejects a create that sets them, and the Cloud Run substrate
  caps total environment size near 32 KiB — which is why packs above the inline limit are
  staged to GCS instead.
- Does it depend on a v1beta1 API field? The Go client is generated from the published
  protobufs, and some documented fields (`agentCard`, for one) exist only in the REST
  discovery document, with no field to set.
- Updates are whole-spec replacements, not partial patches — the container spec lives in a
  protobuf `oneof`, so the full desired state is re-sent on every change.

## Documentation Impact

**Would closing this make something in the docs untrue?**
Check the docs for statements this issue would invalidate — a documented limitation,
a "not supported yet", or a described behaviour that changes. Docs deliberately carry
no issue links, so nothing points back here when this closes; the doc update has to be
part of the work.

This matters more than usual here: the docs list known gaps explicitly — unimplemented
`destroy`/`status`/`import`, `image_mode: cloudbuild` being config-only, the unsupported
tool modes, roles other than `llm` having no effect. Implementing any of those makes a
documented statement false the moment it merges.

This repo's `docs/` is republished into the PromptArena documentation site, so a fix
made there reverts on the next fetch — this repo is the only place it sticks.

- [ ] No documented statement changes
- [ ] Yes — the pages to update are listed below, and updating them is part of this issue

```
<paths under docs/src/content/docs/ that this makes stale>
```

## Checklist

- [ ] I have searched existing issues and feature requests to ensure this is not a duplicate.
- [ ] I have considered backward compatibility.
- [ ] I have provided concrete use cases for this feature.
- [ ] I have checked whether this makes any existing documentation untrue, and listed the pages if so
