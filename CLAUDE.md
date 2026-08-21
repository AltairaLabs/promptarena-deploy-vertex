# promptarena-deploy-vertex — Claude Code project instructions

## Documentation is published without review

Anything under `docs/` reaches readers on the next build, and there is no review
step between writing it and publishing it. Write for someone outside the project.

- **No internal tracking.** Issue numbers, "Day N of the roadmap", milestone
  names. If a limitation is worth documenting, describe the limitation.
- **No links a reader cannot follow.** Local-only backlog files, proposals cited
  by section number.
- **No working notes.** "Spike", "v1/v2", "deferred to", single-trial findings
  written as if settled.
- **No competitor comparisons.** Say what this does and when it fits. Naming
  another tool to say it is worse ages badly and is often wrong on the details;
  naming one as a vocabulary reference ("if you know X's terminology") is fine.
- **Run the commands.** Every flag in a doc is a claim about the CLI. Check them;
  flags get renamed and docs do not notice.
- **No issue links, open or closed.** Docs are not a tracker view. An issue cited
  as a pending limitation becomes a lie the moment it closes, and nothing in the
  docs build notices — four voice caveats pointed at issues closed two months
  earlier, telling readers a shipped feature was missing.

**The other half of this rule lives on the issue.** If closing an issue would
make a documented statement untrue, updating those docs belongs in that issue's
acceptance criteria. Docs carry no links back to tickets, so the link has to run
ticket → docs or it does not exist.

`.claude/skills/docs-review/` has the full review procedure.

