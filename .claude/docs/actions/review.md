# Action: review a branch / diff

What blocks a merge. Read this plus **one** other file — `code-reviewer` adds
[review-style.md](review-style.md), `security-reviewer` adds [review-security.md](review-security.md)
([lenses](README.md)).

Reviewing is read-only: report the fix, do not apply it. Do not run commands that change the working
tree, install packages or launch the app.

This file mirrors rules from [AGENTS.md](../../../AGENTS.md),
[TDD_AND_QUALITY.md](../../../docs/engineering/TDD_AND_QUALITY.md) and
[APPLICATION_ARCHITECTURE.md](../../../product/APPLICATION_ARCHITECTURE.md); when they change, `/update-rules`
updates this list.

---

## Critical — blocks merge

### Files and deletion (DEL-01–05)

- Any code path that deletes, moves or overwrites user payload without an explicit list of files and a
  separate confirmation. Removing a torrent entry keeps files by default; `Drop()` in the engine is
  not deletion.
- A physical path that reaches the filesystem without validation in Go: save-path boundary, symlinks,
  collisions, files shared with another torrent. A path coming from React is never trusted.
- Tests or probes touching anything but their own synthetic files in a temporary directory or
  `testdata/`.

### Module boundaries

- React reaching the filesystem, the engine or generated bindings outside `frontend/src/bridge`.
- React duplicating a Go policy (pause, upload, wanted) or deciding that a write succeeded.
- The Wails facade (`internal/app`) passing engine types, pointers or channels to TypeScript, running
  shell commands on request from the UI, or skipping DTO re-validation.
- A Go service writing SQL or importing anacrolix directly; SQL outside `internal/library`; anacrolix
  types outside `internal/torrentengine`.

### Quality gate (QA-01, QA-02, QA-08)

- Behavior without a test written first, or a report without RED evidence for it.
- Own executable code below 100%: Go by raw counters in the merged profile, TypeScript on four metrics
  per file. Lowered thresholds, narrowed source lists, ignore directives for own logic, disabled or
  skipped tests are blockers regardless of the percentage. `go tool devtool check` must end with
  `GATE PASSED`; a `check fast` or `check offline` run is not acceptance.
- `frontend/wailsjs/` edited by hand, or a changed Go facade without regenerated, committed bindings.
- A TODO, stub or workaround that defers required quality; a task marked `done` with a check that is
  `not_run` or `fail`.

### Documentation in the same change (QA-04, AI-06)

- A behavior, contract, command or schema change without its `docs/<domain>/<feature>.md`, the index
  in `docs/README.md`, a `docs/decisions/` ADR for a significant choice, or the verification report.
- A task status changed in its file but not in the `product/tasks/README.md` navigation, or the other way round.
- Repository text that is not English.

### Dependencies

- A new or bumped package version published less than 14 days ago, a pre-release, a floating range in
  `frontend/package.json`, a new npm install script outside `allowScripts`, or a change to
  `.npmrc`'s `min-release-age` — see [toolchain-versions](../../../docs/decisions/toolchain-versions.md).
- A dependency change without the version table in that ADR updated.

### Personal data

- Committed `.torrent` files, downloaded data, working databases, logs or anything from the owner's
  own downloads.

---

## Warnings

Not rule violations but real: performance risks, unclear names, duplication worth extracting, a test
that asserts existence instead of behavior. Report them separately so a blocker is never buried beside
a suggestion.

---

## Reporting

Cite `file:line`, state the problem, show the fix; for a data-safety or security finding add the
concrete path that triggers it. Skip `package-lock.json`, `go.sum`, binaries and generated
`frontend/wailsjs/` content unless the finding is that they are out of sync. Do not review code the
change did not touch.
