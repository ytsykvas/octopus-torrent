Start a numbered OctopusTorrent task: find it, load its context, and plan it in plan mode before any edit.

`$ARGUMENTS` — a task number (`004`) or empty for the next one.

## Step 1: Pick the task

- With a number: `product/tasks/<NNN>-*.md`.
- Without: the first row of the navigation table in `product/tasks/README.md` with status `planned`
  whose hard dependencies are all `done`. If a task is `in_progress`, name it and ask whether to
  continue it instead.

## Step 2: Read before planning (QA-07)

`AGENTS.md`; `docs/engineering/TDD_AND_QUALITY.md`; `product/APPLICATION_ARCHITECTURE.md`; the task
file in full; every document in its "Read before starting" list; the requirement IDs it names in
`product/PROJECT_REQUIREMENTS.md`; the open decisions it owns in
`docs/decisions/project-baseline.md`; the feature doc and the actual code it touches; `git log -10`.

If the task changes anything the `release-guard` hook protects — `internal/devtool/`, `cmd/devtool/`,
`go.mod`, `go.sum`, an `.npmrc`, `.claude/settings*.json`, `.codex/` — and the session is not in the
`default` permission mode, tell the owner before planning: those edits are blocked until they switch
to `default` (Shift+Tab) and approve them.

## Step 3: Plan in plan mode

Enter plan mode and write the plan per [planning.md](../docs/planning.md): boundaries and contracts
that change, what is reused, refactoring needed, the RED tests first with their expected failure, how
100% coverage will be proven, which documents change, and the verification commands. Ask the owner
only what the documents cannot answer.

## Step 4: After approval

Set the status to `in_progress` in the task file, run `go tool devtool plan-nav` to regenerate the
navigation table, and put the approved plan into the "session plan" of `docs/verification/<NNN>-<slug>.md`. Then work
RED → GREEN → REFACTOR.
