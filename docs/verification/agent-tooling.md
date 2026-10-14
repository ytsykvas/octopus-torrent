# Report: agent tooling and the release-guard hook

**Task:** none (repository-wide tooling, owner's request of 15 October 2026)  
**Date:** 15 October 2026  
**Change version:** the agent-tooling commit (parent `88204db`)  
**Feature:** [agent-tooling](../features/agent-tooling.md); **ADR:** [agent-tooling](../decisions/agent-tooling.md)

## Session architecture plan

- **Request:** the same agent infrastructure as the owner's `~/work/planner` (`.claude` agents,
  commands, hooks and action docs, `.codex`, `.octopus`, `.conductor`), adapted to this project.
  Taken from planner: the structure, single-source action files, plain-path runner prompts, review
  lenses. Not taken: everything Rails/Xibo/monitoring/database-specific.
- **Owner's decisions:** runners octopus, Codex, Conductor; PR instructions for `main`, push/PR
  skipped while there is no remote; agents `code-reviewer`, `security-reviewer`, `tech-lead`,
  `macos-qa`; one hook, protecting the 14-day release cooldown; executable logic in Go so the 100%
  coverage rule applies.
- **Boundaries:** new packages `internal/devtool` (pure `Decide`, `CheckLinks`, `CheckPointers`,
  `CodexAgentTOML`; `Main` dispatch; `System` interface) and `cmd/devtool` (composition root, a `tool`
  in `go.mod`). The product code is untouched.
- **Reuse:** the `.app` coverage technique from 003 (an instrumented binary writing into the test's
  coverage directory); the `RuntimeCheck` dependency-injection pattern for `System`; the slug rules of
  the link check used since 001.

## Source inventory (own executable code added)

| File | Role | Measured by |
|---|---|---|
| `internal/devtool/releaseguard.go` | hook decisions | Go coverage, unit tests |
| `internal/devtool/devtool.go` | `Main`, hook I/O, workspace, `check-docs`, `codex-agents` | Go coverage, unit tests |
| `internal/devtool/docs.go` | link and pointer checks | Go coverage, unit tests |
| `internal/devtool/codexagents.go` | Codex agent rendering | Go coverage, unit tests |
| `internal/devtool/system.go` | OS adapter | Go coverage, unit tests on the real OS |
| `cmd/devtool/main.go` | composition root | Go coverage of the instrumented binary |
| `.octopus/scripts/setup.sh`, `run.sh` | two-command hand-overs, no branches | `TestRunnerScriptsBuildDevtoolAndHandOverToIt` executes every line against a fake `go` |

Configuration (not executable code): `.claude/settings.json`, `.codex/hooks.json` — executed by
`TestConfiguredHooksBlockWithExitStatusTwo`; `.conductor/settings.toml`, `.codex/agents/*.toml`,
`.octopus/project.json` — parsed (check 13); `.codex/agents/*.toml` — compared with their generator by
`TestCodexAgentMirrorsAreUpToDate`.

## RED: tests before the implementation

| Step | Test | Command | Result (expected `fail`) |
|---|---|---|---|
| 1 | `internal/devtool/*_test.go` (guard table, hook contract, workspace, OS adapter) | `go test ./internal/devtool/` | `undefined: HookEvent`, `undefined: Decide`, `undefined: Main`: build failed |
| 2 | `cmd/devtool/main_test.go` (real binary, `exec` replacement, runner scripts) | `go test ./cmd/devtool/` | `no non-test Go files in cmd/devtool`; `.octopus/scripts/setup.sh: No such file or directory` |
| 3 | `TestConfiguredHooksBlockWithExitStatusTwo` (regression for a bug found in verification) | `go test ./cmd/devtool/ -run Configured`, hook command set to `go run ./cmd/devtool …` | `exit 1 … want 2` — `go run` turns exit status 2 into 1, so the hook would never block |
| 4 | allowed: `sed -i` on a report mentioning `.npmrc`, `tee` to `notes-about-.npmrc-files.txt`, `cp frontend/.npmrc.example …` (false positives hit in the live session) | `go test ./internal/devtool/ -run Allows` | 3 × `denied: Shell writes to .npmrc are blocked` |
| 5 | review findings, first round: `--before`, `min-release-age-exclude` (flags, `npm_config_*`, `export`, `npm config set`, `.npmrc` lines), npm value flags shifting the subcommand, `@v1`/`@>=`, `GOPRIVATE`, wrappers (`env`, `time`, `sudo`, `xargs`, `sh -c`), `>`-without-space, `>|`, pnpm/yarn/bunx, `npm create`/`init`, `.NPMRC`, MultiEdit, and false positives on commit messages and greps | `go test ./internal/devtool/` | 34 failing cases (`allowed, want deny` / `denied` for text) |
| 6 | review findings, second round: `go mod tidy`, `-mod=mod`, `--u`, `go -C`, branch versions, `go mod download @latest`, any `GOSUMDB`, `npx … --no` / `--yes`, `npm exec --yes`, `command`/absolute-path/`(…)`, `pushd`, `install-test`, git/URL specs, `npm_config_min-release-age`, `.npmrc` sections and arrays, owner `ask` for `.npmrc` and hook configs, `npm` in a subdirectory of the project | `go test ./internal/devtool/` | 39 failing cases |
| 7 | `ask` delivered as JSON; blocked when it cannot be written | `go test ./internal/devtool/ -run Asks` | `stdout "" is not JSON` |
| 8 | `CheckLinks`, `CheckPointers`, `CodexAgentTOML`, `check-docs`, `codex-agents` | `go test ./internal/devtool/` | `undefined: CodexAgentTOML`, `undefined: CheckLinks`, `undefined: CheckPointers` |
| 9 | automated security review of the commit: guard sources and other settings layers not protected, `$(…)`/backticks in double quotes, `eval`, `bash -lc`, `dash`, shells reading stdin, `$VAR` as the program, a command given as an argument list, unreadable input of a guarded tool let through | `go test ./internal/devtool/` | 29 failing cases |
| 10 | second automated security review: an argument list joined without quoting (a `-c` script split apart), `bash -o pipefail -c`, `+o`, `--rcfile`, `-c -e`, `/dev/stdin` and `/dev/fd/*` scripts, `source /dev/stdin`, `<<<` and `"<<EOF"` taken as here-documents, an apostrophe inside double quotes hiding `$(…)`, backslash escapes | `go test ./internal/devtool/` | 14 failing cases |
| 11 | third automated security review: arithmetic `$((…))` skipped with commands inside, `((…<<EOF))` and partially parsed delimiters (`E"O"F`) or a missing terminator stripping real commands, here-documents fed to non-shell programs that run commands (`xargs sh -c`), `bash -s` / `-i` with an operand | `go test ./internal/devtool/` | 7 failing cases |
| 12 | owner's report: in the `auto` permission mode the "ask" for guard changes was approved without them (this session runs in `auto`, confirmed from `~/.claude/settings.json` `defaultMode` and the hook payload's `permission_mode`) | `go test ./internal/devtool/` | `unknown field PermissionMode`; then `want 2 naming the permission mode` |

## GREEN: actual checks

| # | Check | Command | Result |
|---|---|---|---|
| 1 | Unit tests | `go test ./internal/devtool/` | `pass`, 38 tests (67 allowed and 131 denied commands, `.npmrc` write/edit/MultiEdit and guard-file cases) |
| 2 | Binary, script, hook-config and Codex-mirror tests | `go test ./cmd/devtool/` | `pass`, 5 tests; the binary is built once per run and removed afterwards |
| 3 | Full Go suite | `go test ./...` | `pass` |
| 4 | Merged Go coverage | README procedure: unit + devtool binary + instrumented `.app` | `pass`: 0 blocks with a zero count; every package 100.0% |
| 5 | Formatting and analysis | `gofmt -l .`; `go vet ./...` | `pass`: empty; clean |
| 6 | Hook latency | `go tool devtool hook release-guard` with a JSON payload, 3 runs | `pass`: 0.05 s each |
| 7 | Live hook in Claude Code | Bash tool: `npm --prefix frontend view react version --min-release-age=0` | `pass`: blocked with "Overriding min-release-age below 14 days is blocked…" |
| 8 | Live allow / `.npmrc` edit | payloads for `npm --prefix frontend ci` and an Edit lowering `min-release-age` to 7 | `pass`: exit 0; exit 2 with the `.npmrc` reason; the file unchanged |
| 9 | `setup.sh` in a fresh worktree | `git worktree add` + current changes; `sh .octopus/scripts/setup.sh` | `pass`: 2.9 s; `frontend/dist` and `build/bin/devtool` created |
| 10 | `run.sh` in that worktree | `OCTOPUS_PORT=41234 sh .octopus/scripts/run.sh` | `pass`: port 41234 listening after 9 s; the runner PID is the `wails dev` process itself; Vite on a free port (5174); the dev app window opened |
| 11 | Stop | `kill -TERM <runner pid>` | `pass`: "Caught quit", every worktree process gone after 1 s; unrelated processes untouched. Only SIGTERM was tested; SIGKILL leaves wails, Vite and the dev app running whatever the process chain |
| 12 | Release app still works | `go tool wails build -clean -trimpath`; `open`; click "Check Go connection" | `pass`: "Go go1.27.2 · darwin/arm64" |
| 13 | `macos-qa` method by PID | `open -n` this worktree's app, `pgrep -f <path>`, window of `first process whose unix id is <pid>`, `kill -TERM <pid>` | `pass`: one PID found, window after 1 s, the instance exited on SIGTERM. A ⌘Q keystroke sent through System Events did **not** quit it, so that method was removed from the agent |
| 14 | Config syntax | `tomllib` on `.conductor/settings.toml`, `.codex/agents/*.toml`; `json` on `.codex/hooks.json`, `.octopus/project.json`, `.claude/settings.json` | `pass` |
| 15 | Links and pointers | `go tool devtool check-docs` | `pass`: every relative link, anchor and `.claude` pointer resolves |
| 16 | Generated bindings unchanged | `go tool wails generate module`; `git -c core.fileMode=false status --short frontend/wailsjs` | `pass`: empty (only a file-mode flip, which Wails itself causes) |
| 17 | Codex agents and hook in a Codex session | — | `not_run`: no Codex session was used; the payload shape (`tool_name` `"Bash"`) is assumed from Claude Code's |
| 18 | octopus / Conductor apps importing the configs | — | `not_run`: verified by running the scripts the way the runners do (9–11), not inside the apps |
| 19 | The `ask` prompt in a live session | — | `not_run` as a prompt: this session runs in `auto`, where Claude Code settles asks itself — which is exactly why such changes are now denied there |
| 20 | Guard change in `auto` mode, live | Edit tool on `internal/devtool/system.go` | `pass`: blocked — "The current permission mode (auto) would approve this without the owner, so it is blocked; the owner switches to the default mode to approve it." |

## Reviews

`/branch-review` ran the three lenses (as `general-purpose` agents following
`.claude/agents/*.md`, because project agents load only at session start). Their findings and what
was done:

- **Fixed:** the cooldown escapes npm documents itself (`before`, `min-release-age-exclude`), the
  `.npmrc` parser mismatch (now owner `ask`), Go resolution through `go mod tidy` / `-mod=mod` /
  partial versions, checksum settings in every form, wrapper and subshell forms, npm value flags,
  non-registry specs, broad permission prefixes (now exact commands), the exec-chain rationale,
  `macos-qa` targeting by bundle ID, Codex mirrors claimed but not generated (now generated and
  tested), inline Python and Bash in `/check` (now `devtool check-docs`), `.claude/settings.local.json`
  missing from `.gitignore`, the duplicate binary build in tests, cross-task follow-ups recorded in
  tasks 004, 008 and 014.
- **After the commit**, an automated security review found four more gaps (guard sources and other
  settings layers, substitutions inside quotes and other unanalysed shell forms, argument-list
  commands, fail-open on unreadable input of a guarded tool); all fixed test-first (RED 9). A second
  automated review found five places where the hand-written parser read a command differently from
  the shell; fixed test-first (RED 10). A third found five more; this time the approach changed to
  failing closed: here-document bodies are skipped only when every one goes to `cat`, `git`, `gh` or
  `tee` with a fully parsed delimiter and an existing terminator, otherwise they are decided as
  commands; arithmetic is searched for substitutions instead of skipped (RED 11).
- **Accepted as limits** ([ADR](../decisions/agent-tooling.md)): Codex `apply_patch`, script files and
  unparsed shell forms, writes by unknown programs such as interpreters, explicit-but-young Go
  versions, fail-open when devtool does not compile. The guarantee is task 004's release-age check of
  the lockfiles.

## Refactoring

- `go run` → `go tool devtool` in hook commands (RED 3).
- The guard moved from regular expressions over the whole command text to a quote-aware per-command
  parser (`shellSegments`, `unwrap`), which removed the false positives and most bypasses at once.
- `.npmrc` handling moved from "parse and allow" to "deny what weakens, ask the owner for the rest".
- `/code-review` renamed `/branch-review` (built-in name collision).

## Limitations

See "Limits accepted" in the ADR. Two checks are `not_run` because they need the Codex, octopus or
Conductor applications themselves; one because it would wait on the owner.

## Summary

The tooling works as configured: the hook blocks in a live session and passes 198 command cases, the
runner scripts prepare and start a fresh workspace and stop cleanly, the documentation and Codex
mirrors are checked by Go, and all own Go code stays at 100% by raw counters.
