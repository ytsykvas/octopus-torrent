# Workflow: from a task to a commit

> Last updated: 2026-10-15

Work follows the numbered plan in `product/tasks/`. Every step below has a slash command for Claude
Code; Codex, octopus and Conductor share the same instruction files ([agent tooling](../tooling/agent-tooling.md)).

## Flow

```
/task [NNN]        pick the next ready task, read its context, plan in plan mode
  owner approves   status → in_progress (task file + `go tool devtool plan-nav`)
  stages           RED → GREEN → REFACTOR, each stage committed; report in docs/verification/
/check             `go tool devtool check` → GATE PASSED
/branch-review     code-reviewer + security-reviewer + tech-lead; fix or assign every finding
/qa                optional: macos-qa drives the real .app through Accessibility
/update-docs       docs/<domain>/, docs/README.md, README, ADRs agree with the change
status → done      acceptance boxes, "Execution result", `plan-nav`, `check-plan`
/pushit            docs → rules → safety scan → checks → commit → push and PR (only when asked)
```

## Picking and planning a task

- The next task is a `planned` task whose hard dependencies are all `done`. See the
  [navigation](../../product/tasks/README.md#navigation-of-all-tasks) or the README "Current state".
- The plan, per [planning.md](../../.claude/docs/planning.md), must cover:
  - which boundaries and contracts change, and what is reused;
  - the RED tests and their expected failure;
  - how 100% coverage will be proven;
  - which documents change.
- Plan and task status: [task plan](../tooling/task-plan.md).

## While working

- **One stage, one commit.** Each commit message says what changed and how it was verified
  ([commit-message.md](../../.claude/docs/actions/commit-message.md), which also sets the trailer
  rules).
- **Record as you go.** Each stage gets RED and GREEN commands with `pass`/`fail`/`not_run` in
  `docs/verification/<NNN>-<slug>.md`. An unavailable check is `not_run`, never a guess.
- **Decisions.** A significant choice goes into an ADR in `docs/decisions/` (from
  `_ADR_TEMPLATE.md`), and its row in [docs/README.md](../README.md). An open question from the
  [register](../decisions/project-baseline.md#6-open-decision-register) is closed there.
- **Documents.** Behavior goes into `docs/<domain>/<feature>.md` (from
  [_FEATURE_TEMPLATE.md](../_FEATURE_TEMPLATE.md)) in the same change ([update-docs](#documentation)).

## Documentation

`/update-docs` maps each change to the document that must agree with it. It shows a table and
asks before editing; `go tool devtool check-docs` then checks every link. The domains:

| Domain | What |
|---|---|
| `docs/app/` | the Go application: shell, facade, operations; later services, engine, library |
| `docs/bridge/` | the Go–TypeScript contract and how React calls Go |
| `docs/frontend/` | React structure; later state, i18n, styles, screens |
| `docs/tooling/` | quality gate, devtool, task plan, agent tooling |
| `docs/development/` | getting started, testing, this workflow |

## Language and safety

- **Language:** all repository text is English; conversation with the owner is Ukrainian
  ([repository-language](../decisions/repository-language.md)).
- **Dependencies:** exact versions published at least 14 days ago. No `npx` download, no global
  install, no `@latest`. Never lower `min-release-age` ([dependencies.md](../../.claude/docs/dependencies.md)).
  No hook enforces this any more ([ADR](../decisions/agent-tooling.md#no-pre-tool-hook)).
- **Data:** never use the owner's torrents, downloads or databases; tests use synthetic files.
  Nothing personal or generated is committed.
- **Remote:** push or open a PR only when the owner asks. `origin` is
  `github.com/ytsykvas/octopus-torrent`.
