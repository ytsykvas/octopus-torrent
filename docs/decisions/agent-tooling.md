# Agent tooling: Claude Code, Codex, octopus and Conductor

**Status:** `accepted`  
**Date:** 15 October 2026, owner's decision  
**Task:** none (repository-wide tooling)  
**Requirements:** AI-01, AI-06, AI-07, AI-09, QA-01, QA-02, QA-05

## Question

Agents work on this repository through four tools: Claude Code, Codex, and the parallel-workspace
runners octopus and Conductor. How do they share one set of rules and actions, how is a fresh
workspace prepared, and how is the owner's 14-day dependency cooldown protected from an agent that
reaches for the newest release?

**Revised on 15 October 2026 (owner's decision):** the `release-guard` PreToolUse hook that answered
the last question at first was removed; see "No pre-tool hook" below.

## Decision

### Shared instructions

- **One source of rules.** `AGENTS.md` holds the binding rules; Codex reads it natively and
  `CLAUDE.md` imports it with `@AGENTS.md`, adding only commands, layout and workflow.
- **One source per action.** Review, pull request, commit message, fixing checks, addressing review
  and resolving conflicts live in `.claude/docs/actions/`. Runner configs (`.conductor/settings.toml`
  `[prompts]`, `.octopus/instructions/*`) carry only invariants that must hold unread plus a **plain
  path** — never `@path`, which would load the file into every run.
- **Review lenses.** Three read-only agents split the checklist so each loads only its half:
  `code-reviewer` (architecture & quality), `security-reviewer` (data safety & attack surface),
  `tech-lead` (design & readiness, no checklist). `macos-qa` drives the real `.app` through
  Accessibility, addresses only the instance it launched (by PID — every workspace's dev app shares the
  bundle ID) and reports text, never screenshots.
- **Codex mirrors are generated.** `go tool devtool codex-agents` renders `.codex/agents/*.toml` from
  `.claude/agents/*.md`; a `cmd/devtool` test fails while a mirror is stale.
- **Commands.** `/task` (start a numbered task in plan mode), `/check`, `/branch-review`, `/qa`,
  `/update-docs`, `/update-rules`, `/pushit`. `/branch-review` is not named `/code-review` because
  Claude Code has a built-in command of that name.

### Executable logic in Go

The workspace commands, the documentation and plan checks and the [quality gate](quality-gate.md) are
`cmd/devtool` + `internal/devtool`, tested and counted in the 100% coverage gate like the product.
`cmd/devtool` is a `tool` in `go.mod` and runs as `go tool devtool …`. The only shell left is the two runner scripts in `.octopus/scripts/`, each
two commands (build devtool, `exec` it) with no branches; the `cmd/devtool` test executes every line
of both against a fake `go`, which is the measurement for them under
[TDD_AND_QUALITY §3](../engineering/TDD_AND_QUALITY.md#3-what-exactly-100-coverage-means). `/check` holds no
inline scripts: links and instruction pointers are `go tool devtool check-docs`.

### No pre-tool hook

The first version had a `release-guard` PreToolUse hook (`go tool devtool hook release-guard`, in
`.claude/settings.json` and `.codex/hooks.json`): a hand-written shell parser that denied commands
able to bypass the cooldown (`npx`, global installs, `go get …@latest`, cooldown overrides, writes to
`.npmrc`) and denied any change to its own code, `go.mod` or agent settings in the permission modes
that approve by themselves. The owner removed it on 15 October 2026: it blocked routine work (every
change to `internal/devtool`, `go.mod` or the settings needed a switch to the `default` mode), refused
harmless commands, and every review — three before task 004, two during it — found new shell forms it
misread. Its history and evidence stay in the [verification report](../verification/agent-tooling.md)
and in [report 004](../verification/004-reproducible-check-commands.md).

The cooldown is enforced where it does not depend on reading shell commands:

- `frontend/.npmrc` → `min-release-age=14`: npm itself refuses younger versions of any package,
  including transitive ones; install scripts are off unless listed in `allowScripts`.
- The [quality gate](quality-gate.md) on every `go tool devtool check`: npm's effective cooldown
  settings, Go's module settings (`go env`), the release age of every locked npm version and every Go
  module, that every locked npm artifact is the registry's, `npm audit` and `govulncheck`.
- The rules for agents in [dependencies.md](../../.claude/docs/dependencies.md): exact versions,
  `npm --prefix frontend`, no `npx` download, no global installs, no `@latest`.

### Workspaces

`setup`: `npm ci`, `frontend/dist` (embedded by `main.go`), `go mod download`. `run`: resolve the
`wails` binary with `go tool -n wails` and `exec` into `wails dev -devserver localhost:$OCTOPUS_PORT`
(or `$CONDUCTOR_PORT`), so the runner's PID is `wails dev` itself and its stop signal goes there
directly. No `archive` step: nothing is created outside the worktree.

### Base branch and remote

PR instructions target `main`. `origin` is the owner's GitHub repository
`https://github.com/ytsykvas/octopus-torrent` (added on 15 October 2026); in a copy without a remote,
`/pushit` commits locally and reports that push and PR were skipped.

## Limits accepted

Without the hook nothing stops a command before it runs. An agent that ignores
[dependencies.md](../../.claude/docs/dependencies.md) can execute code younger than 14 days that never
reaches a lockfile — `npx <package>`, `npm i -g`, `go run <module>@latest`, `go install …@latest` —
or override the cooldown for one command (`--min-release-age=0`, `npm_config_*`). The gate only sees
what ends up in `frontend/package-lock.json`, `go.mod`/`go.sum` and the effective settings. Review of
the transcript and of the diff is the remaining control for those commands.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Copy planner's setup as is (Rails, Xibo, monitoring) | Fast | Domain-specific rules and scripts that mean nothing here | Only the structure was reused |
| Hook and runner scripts in Bash (as in planner) | Familiar, no build step | Own executable code without a coverage tool; the policy requires 100% | Owner chose Go |
| Keep the `release-guard` hook | Stops `npx`, global installs and cooldown overrides before they run | A hand-written shell parser: new gaps in every review, false refusals, its self-protection blocked routine edits in `auto`/`acceptEdits` | Removed by the owner on 2026-10-15 |
| Replace the parser with `mvdan.cc/sh/v3/syntax` | Real shell grammar | A new dependency, and the hook still blocks routine work | Not taken with the removal |
| Broad prefix permissions (`go test:*`, `git diff:*`) | Fewer prompts | `go test -exec`, `go vet -vettool`, `git diff --output` run or write anything with no prompt | Allow exact commands only |
| Architecture and testing guides under `.claude/docs` | Self-contained agent docs | Copies of `AGENTS.md`, the architecture and the TDD policy that drift | Link instead; only `review*.md` mirror rules, kept in step by `/update-rules` |

## Verified grounds

See the [verification report](../verification/agent-tooling.md): the runner scripts run in a fresh
git worktree with a clean stop, and — for the removed hook — its RED evidence and a live block in
Claude Code. The removal was verified by a probe that the hook no longer intercepts and by a full
`go tool devtool check`.

## Consequences

- Changing the dependency policy means changing `minReleaseAgeDays` (`internal/devtool/deps.go`)
  test-first, with [toolchain-versions](toolchain-versions.md) and `frontend/.npmrc`.
- Task [004](../../product/tasks/004-reproducible-check-commands.md) built the single check command
  and the release-age check in `internal/devtool` ([quality-gate](quality-gate.md)); `/check` runs it.
- Every workspace (and every test) gets its own library location, never the owner's real library.
  [008](../../product/tasks/008-sqlite-driver-durability-spike.md) fixed the contract
  ([sqlite-driver](sqlite-driver.md)): `OCTOPUS_DATA_DIR`, which `workspace run` will set per
  workspace; the first task that reads the data directory (014 or 028) implements both sides.
