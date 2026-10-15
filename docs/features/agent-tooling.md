# Agent tooling and the release-guard hook

**Status:** `implemented`  
**Requirements:** AI-01, AI-06, AI-07, QA-01, QA-02, QA-05  
**Related tasks:** none (repository tooling); [004](../../product/tasks/004-reproducible-check-commands.md) will replace the `/check` step list  
**Last verified version:** see `git log -1 -- internal/devtool`

## Goal and behavior

Developer tooling, not a product feature: agents (Claude Code, Codex) and parallel-workspace runners
(octopus, Conductor) share one set of rules, actions and review agents, prepare and start a
workspace with one command each, and cannot weaken the 14-day dependency release cooldown by
accident. Index of what is where: [.claude/docs/README.md](../../.claude/docs/README.md); reasoning:
[ADR](../decisions/agent-tooling.md).

## Architecture analysis before the change

Separate from the product: `internal/devtool` and `cmd/devtool` import nothing from the app, and
nothing in the app imports them. State: none — every command is a pure decision or a short process
hand-over. Side effects are behind the `System` interface (read a file, run, capture output,
replace the process).

## Contracts and data

- **Hook input:** the PreToolUse JSON on stdin — `cwd`, `tool_name`, `tool_input.command`,
  `tool_input.file_path`, `content`, `old_string`, `new_string`, `replace_all`.
- **Hook output:** exit 0 allows; exit 2 with `release-guard: <reason> Policy: …` on stderr blocks;
  exit 0 with `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask",…}}` on
  stdout hands the call to the owner (every `.npmrc` change that does not weaken the cooldown, and
  every change to the guard's settings, source or module files) — or, when the payload's
  `permission_mode` would approve that ask on its own (`auto`, `acceptEdits`, `bypassPermissions`),
  blocks it with exit 2; input that is not JSON is allowed with
  a warning; a guarded tool whose input cannot be read is blocked; an ask that cannot be written
  blocks. A command may arrive as a string or a list of arguments.
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
| Commands that keep or bypass the cooldown | `TestDecideAllows…`, `TestDecideDenies…` | undefined `Decide` |
| `.npmrc` written or edited | `TestDecideChecksTheResultOf…` | undefined `Decide` |
| Hook exit contract, unreadable input | `TestHook…` | undefined `Main` |
| Setup order and first failure; run port and `exec` | `TestWorkspace…` | undefined `Main` |
| Real binary, `exec` replaces the process, runner scripts | `cmd/devtool/main_test.go` | no `main.go`, no scripts |
| Configured hook commands block with exit 2 | `TestConfiguredHooksBlockWithExitStatusTwo` | exit 1 with `go run` |
| Review findings (two rounds, then the commit security review) | guard tables, `TestDecideAsksTheOwner…`, `TestHookReads…`, `TestHookBlocksAKnownTool…` | 34, 39, then 29 failing cases |
| Owner `ask` as JSON | `TestHookAsksTheOwnerThroughJSONOnStdout`, `TestHookBlocksWhenTheAskCannotBeWritten` | empty stdout |
| Links, pointers, Codex mirrors | `docs_test.go`, `codexagents_test.go`, `TestCodexAgentMirrorsAreUpToDate` | undefined functions |

## Implementation and reuse

`releaseguard.go` (decision), `devtool.go` (`Main`, hook, workspace, doc commands), `docs.go`
(links, pointers), `codexagents.go` (Codex rendering), `system.go` (`OSSystem`),
`cmd/devtool/main.go` (composition root). Configs: `.claude/settings.json`, `.codex/hooks.json`
(`go tool devtool hook release-guard`), `.octopus/scripts/{setup,run}.sh`, `.conductor/settings.toml`.

## Checks and 100% coverage

`go test ./...`; merged coverage per the README procedure (the `cmd/devtool` test writes its
instrumented binary's counters into the unit coverage directory). Results:
[verification report](../verification/agent-tooling.md).

## Refactoring and debt check

No open debt. The hook's known blind spots are recorded in the ADR as accepted limits of a safety net;
the guarantee they need (the age of every locked version) is task 004's scope, written into that task.

## Usage and recovery

- A blocked command names the rule; use the exact-version, `--prefix frontend` or `--no-install` form.
- If `devtool` stops compiling, the hook exits 1 and agents continue (non-blocking); fix it with
  `go test ./internal/devtool/ ./cmd/devtool/`.
- Runner: octopus uses `.octopus/` as is; Conductor reads `.conductor/settings.toml`.

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-15 | First version | [report](../verification/agent-tooling.md), [ADR](../decisions/agent-tooling.md) |
