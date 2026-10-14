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

The hook, the workspace commands and the documentation checks are `cmd/devtool` +
`internal/devtool`, tested and counted in the 100% coverage gate like the product. `cmd/devtool` is a
`tool` in `go.mod` and runs as `go tool devtool …`, which passes exit status 2 through (`go run`
turns every failure into 1, which agents treat as non-blocking — a regression test runs the
configured hook commands). The only shell left is the two runner scripts in `.octopus/scripts/`, each
two commands (build devtool, `exec` it) with no branches; the `cmd/devtool` test executes every line
of both against a fake `go`, which is the measurement for them under
[TDD_AND_QUALITY §3](../engineering/TDD_AND_QUALITY.md#3-what-exactly-100-coverage-means). `/check` holds no
inline scripts: links and instruction pointers are `go tool devtool check-docs`.

### The `release-guard` hook

PreToolUse on Bash, Edit, MultiEdit and Write (Codex: Bash). Three outcomes:

- **Deny** (exit 2, reason on stderr):
  - an npm `min-release-age` below 14 days, `before`, `min-release-age-exclude`, `userconfig`,
    `globalconfig` or `registry`, as a flag or as an `npm_config_*` variable (any case or dash form,
    including `export`);
  - `npm config set|delete|edit` of the cooldown keys;
  - global npm installs, `npm create` / `npm init <initializer>`, `npm exec` without `--no` or with
    `--yes`, `npx` without `--no-install` or with `--yes`, pnpm, yarn and bun;
  - npm installs of anything but a registry package (git, GitHub, URL, file specs skip the age
    filter);
  - npm install-family commands whose project (`package.json` directory, found the way npm finds it)
    has no `.npmrc` that keeps the cooldown;
  - `go get` without an explicit version, any version query that is not a full `vX.Y.Z` /
    pseudo-version / `none` (`@latest`, `@v1`, branches, ranges), `go get -u`, `go mod tidy`, and
    `-mod=mod` (both add missing modules at their latest version);
  - any `GOSUMDB` but the default, `GONOSUMDB`, `GONOSUMCHECK`, `GOINSECURE`, `GOPRIVATE`,
    `-insecure` — as an assignment, `export` or `go env -w`;
  - shell writes (redirection, `tee`, `cp`, `mv`, `rm`, `sed -i`, …) to any `.npmrc` or to the guard
    (see "Ask the owner");
  - a shell that reads its commands from stdin (`… | sh`, `bash <<EOF`) and a command name taken from a
    variable (`$PM install …`) — neither can be checked;
  - an `.npmrc` edit whose result has `min-release-age` below 14, `before`, `min-release-age-exclude`,
    sections, arrays or quotes.
- **Ask the owner** (JSON `permissionDecision: "ask"` on stdout): every other change to an `.npmrc`, and
  every change to what can define, disable or rewrite the guard — `.claude/settings.json`,
  `.claude/settings.local.json`, `.codex/hooks.json`, `.codex/config.toml`, `internal/devtool/`,
  `cmd/devtool/`, `go.mod`, `go.sum`. npm's own parser decides what an `.npmrc` means, so the owner —
  not this parser — approves any change to one.
- **Allow** everything else.

Commands are parsed with quotes: wrappers (`env`, `sudo`, `time`, `nice`, `command`, `exec`,
`xargs`), `sh|bash|zsh|dash|ksh -c` (also inside combined flags such as `-lc`), `eval`, `$(…)` and
backtick substitutions (also inside double quotes), subshells, `cd`/`pushd` and absolute program paths
are followed. Here-document bodies are data and are skipped, and only real npm flags and real
assignments are checked, so commit messages and greps that mention the settings are not refused. A
command given as an argument list is joined; a guarded tool whose input cannot be read is blocked,
while a payload that is not JSON at all is let through with a warning.

### Workspaces

`setup`: `npm ci`, `frontend/dist` (embedded by `main.go`), `go mod download`. `run`: resolve the
`wails` binary with `go tool -n wails` and `exec` into `wails dev -devserver localhost:$OCTOPUS_PORT`
(or `$CONDUCTOR_PORT`), so the runner's PID is `wails dev` itself and its stop signal goes there
directly. No `archive` step: nothing is created outside the worktree.

### Base branch and remote

PR instructions target `main`; `/pushit` commits locally and reports that push and PR were skipped
until the owner creates a remote.

## Limits accepted

The hook is a **safety net, not a sandbox**: it stops the usual and the expedient ways around the
policy, including the ones npm suggests in its own error message. It does not see:

- Codex file edits (`apply_patch`); the Codex hook payload itself is unverified (no Codex session was
  used) — if Codex names its shell tool differently, the guard allows everything there.
- Script files the agent writes and runs (`bash script.sh`, `source`), shell forms beyond the parser,
  writes to protected files by programs it does not know (an interpreter such as `python3` writing a
  file), or a dependency added by hand to `package.json` / `go.mod` and resolved later.
- A Go version that is explicit and full but younger than 14 days.
- Its own source when changed outside the Edit tools (see above); Edit/Write changes ask the owner. If
  devtool fails to compile, the hook exits 1 and agents continue (fail-open, so a broken hook cannot
  lock an agent out of the tools it needs to fix it).

The guarantee therefore comes from checking results: **task 004** adds a release-age check of every
locked npm package and Go module and of npm's effective cooldown settings, implemented in
`cmd/devtool` with the same `minReleaseAgeDays` constant. Until then, the release date is checked by
hand ([.claude/docs/dependencies.md](../../.claude/docs/dependencies.md)).

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Copy planner's setup as is (Rails, Xibo, monitoring) | Fast | Domain-specific rules and scripts that mean nothing here | Only the structure was reused |
| Hook and runner scripts in Bash (as in planner) | Familiar, no build step | Own executable code without a coverage tool; the policy requires 100% | Owner chose Go |
| `go run ./cmd/devtool` in hook commands | No `go.mod` change | Exit status 2 becomes 1: the guard would never block | Found in verification; regression test added |
| Parse every `.npmrc` change and allow the ones that look right | No prompt for the owner | npm reads sections, inline comments and quotes differently from any simple parser (shown by the security review) | The owner approves `.npmrc` changes |
| Broad prefix permissions (`go test:*`, `git diff:*`) | Fewer prompts | `go test -exec`, `go vet -vettool`, `git diff --output` run or write anything with no prompt | Allow exact commands only |
| Architecture and testing guides under `.claude/docs` | Self-contained agent docs | Copies of `AGENTS.md`, the architecture and the TDD policy that drift | Link instead; only `review*.md` mirror rules, kept in step by `/update-rules` |

## Verified grounds

See the [verification report](../verification/agent-tooling.md): RED evidence for every rule, 100%
raw-counter coverage, the configured hook commands executed by a test, a live block in Claude Code,
and the runner scripts run in a fresh git worktree with a clean stop.

## Consequences

- Changing the dependency policy means changing `internal/devtool/releaseguard.go` test-first, with
  this ADR and [toolchain-versions](toolchain-versions.md).
- Task [004](../../product/tasks/004-reproducible-check-commands.md) builds the single check command
  and the release-age check in `cmd/devtool`, replacing the step list in `/check`.
- Tasks [008](../../product/tasks/008-sqlite-driver-durability-spike.md) and
  [014](../../product/tasks/014-sqlite-schema-migrations.md) must give every workspace (and every
  test) its own library location, never the owner's real library; `workspace run` passes it.
