# Agent tooling

**Status:** `implemented`  
**Requirements:** AI-01, AI-06, AI-07, QA-01, QA-02, QA-05  
**Related tasks:** none (repository tooling); [004](../../product/tasks/004-reproducible-check-commands.md) added `check-plan`, `plan-nav` and the [quality gate](quality-gate.md)  
**Last verified version:** see `git log -1 -- internal/devtool`  
**Last updated:** 2026-10-15 (moved from `docs/features/agent-tooling.md`)  
**See also:** [devtool command reference](devtool.md), [task plan](task-plan.md), [workflow](../development/workflow.md)

## Goal and behavior

Developer tooling, not a product feature: agents (Claude Code, Codex) and parallel-workspace runners
(octopus, Conductor) share one set of rules, actions and review agents, prepare and start a
workspace with one command each. The 14-day dependency cooldown is enforced by `frontend/.npmrc` and
the [quality gate](quality-gate.md); the `release-guard` hook that also refused commands before they
ran was removed on 2026-10-15 ([ADR](../decisions/agent-tooling.md#no-pre-tool-hook)). Index of what is where: [.claude/docs/README.md](../../.claude/docs/README.md); reasoning:
[ADR](../decisions/agent-tooling.md).

## Architecture analysis before the change

Separate from the product: `internal/devtool` and `cmd/devtool` import nothing from the app, and
nothing in the app imports them. State: none — every command is a pure decision or a short process
hand-over. Side effects are behind the `System` interface (read a file, run, capture output,
replace the process).

## Contracts and data

- **Doc checks:** `devtool check-docs` prints `DEAD <file>: <target>` per broken link, anchor or
  `.claude/**.md` pointer and exits 1; `devtool codex-agents` writes `.codex/agents/<name>.toml` from
  `.claude/agents/<name>.md`.
- **Plan checks (task 004):** `devtool check-plan` prints `PLAN <problem>` per inconsistency in
  `product/tasks` (task headers, unique IDs, links, a dependency cycle by topological sort, a `done`
  or `in_progress` task with an unfinished dependency, unknown or uncovered requirements, the
  coverage matrix and its primary tasks, the scenario table, a stale navigation block) and exits 1;
  `devtool plan-nav` rewrites only the `TASKS-NAV` block from the task files.
- **Workspace:** `devtool workspace setup`; `devtool workspace run` reads `OCTOPUS_PORT`, else
  `CONDUCTOR_PORT` (1–65535). Unknown commands exit 64 with usage.

## Scenarios and TDD

| Scenario | Test | RED |
|---|---|---|
| Setup order and first failure; run port and `exec` | `TestWorkspace…` | undefined `Main` |
| Real binary, `exec` replaces the process, runner scripts | `cmd/devtool/main_test.go` | no `main.go`, no scripts |
| Links, pointers, Codex mirrors | `docs_test.go`, `codexagents_test.go`, `TestCodexAgentMirrorsAreUpToDate` | undefined functions |

## Implementation and reuse

`devtool.go` (`Main`, workspace, doc and plan commands), `docs.go` (links, pointers),
`codexagents.go` (Codex rendering), `system.go` (`OSSystem`), `cmd/devtool/main.go` (composition
root). Configs: `.claude/settings.json` (permissions), `.octopus/scripts/{setup,run}.sh`,
`.conductor/settings.toml`.

## Checks and 100% coverage

`go tool devtool check` (the `cmd/devtool` test writes its instrumented binary's counters into the
unit coverage directory). Results:
[verification report](../verification/agent-tooling.md).

## Refactoring and debt check

No open debt. What is no longer stopped before it runs without the hook is recorded in the ADR as an
accepted limit.

## Usage and recovery

- Dependency commands follow [dependencies.md](../../.claude/docs/dependencies.md): exact versions,
  `--prefix frontend`, no `npx` download, no global installs.
- Runner: octopus uses `.octopus/` as is; Conductor reads `.conductor/settings.toml`.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-15 | First version | [report](../verification/agent-tooling.md), [ADR](../decisions/agent-tooling.md) |
| 2026-10-15 | `release-guard` hook removed (owner's decision); `Main` no longer reads stdin | [ADR](../decisions/agent-tooling.md#no-pre-tool-hook), `go tool devtool check` |
