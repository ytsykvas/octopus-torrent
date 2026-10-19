# devtool command reference

> Last updated: 2026-10-15

`go tool devtool` is the project's own developer CLI: `cmd/devtool` is the composition root and
`internal/devtool` holds the logic. It runs through the `tool` directive in `go.mod`, so nothing is
installed globally. Every side effect goes through the `System` interface, so it is tested like
product code (100%). All commands run from the repository root.

| Command | Does | Output and exit code |
|---|---|---|
| `check` | the full quality gate: every step, never stops early ([quality gate](quality-gate.md)) | one line per step; `GATE PASSED` → 0, otherwise `GATE FAILED…` → 1 |
| `check offline` | the gate without network steps; `npm ci --offline` | best `GATE INCOMPLETE: not run: …` → 0 |
| `check fast` | also without the `.app` coverage run (TDD loop); `npm ci --offline` | best `GATE INCOMPLETE` → 0 |
| `check deps` | only the network steps: release age, `npm audit`, `govulncheck`, updates out of quarantine | `DEPENDENCIES PASSED` → 0; `DEPENDENCIES FAILED` → 1, also when a step is `not_run` (no network) |
| `check-docs` | every relative Markdown link and anchor in Git files, and every `.claude/**.md` pointer in `CLAUDE.md`, `AGENTS.md`, `.claude/`, `.codex/`, `.octopus/`, `.conductor/` and `docs/`, resolves | `DEAD <file>: <target>` per problem → 1; `pass …` → 0 |
| `check-plan` | the task plan is consistent ([task plan](task-plan.md)) | `PLAN <problem>` per problem → 1; `pass plan: …` → 0 |
| `plan-nav` | regenerates the navigation block of `product/tasks/README.md` from the task files | `plan-nav: navigation regenerated` or `… already up to date` → 0; `PLAN <problem>` lines → 1 |
| `codex-agents` | writes `.codex/agents/<name>.toml` from `.claude/agents/<name>.md` | 0; the Go tests fail if a mirror is stale |
| `workspace setup` | `npm --prefix frontend ci`, `npm --prefix frontend run build`, `go mod download` (stops at the first failure) | 0 or 1 naming the step |
| `workspace run` | replaces itself with `wails dev -devserver localhost:$PORT`; the port is `OCTOPUS_PORT`, else `CONDUCTOR_PORT` (1–65535) | 1 without a valid port |
| anything else | prints usage | 64 |

A command that fails prints `devtool: <reason>` on stderr and exits 1.

## When to run what

- **After editing any Markdown:** `check-docs`.
- **After changing a task file** (status, dependencies): `plan-nav`, then `check-plan`.
- **After changing `.claude/agents/*.md`:** `codex-agents`, and commit the `.codex/agents` change.
- **Before accepting a change:** `check`. Use `check fast` while iterating, and `check deps` when only
  dependencies changed.

## Files written

- `check` writes:
  - `frontend/node_modules` (`npm ci`), `frontend/coverage/`, `frontend/dist`;
  - `frontend/wailsjs/` and `frontend/spike/wailsjs/` (`go tool wails generate module` in each app's
    directory, **tracked**): when the bindings had drifted, the regenerated files stay in the working
    tree, and both `bindings` and `single code version` fail — commit them with the Go change;
  - `cmd/engine-spike/assets` (the spike's page) and the instrumented
    `cmd/engine-spike/build/bin/EngineSpike.app`;
  - `build/coverage/{go-unit,go-app}` and their profiles;
  - `build/bin/OctopusTorrent.app`, rebuilt as a normal release build at the end.
- The runner scripts build `build/bin/devtool`.

Everything except the two `wailsjs/` directories is gitignored.

## Code map

| File | Responsibility |
|---|---|
| `devtool.go` | `Main` dispatch, the `System` interface, the repository listing (`repositoryFiles`, `listedPath`), `sortedKeys` |
| `workspace.go` | the `workspace-setup` and `workspace-run` commands |
| `check.go` | gate modes, the step runner, the report, tool steps |
| `toolchain.go` | the npm cooldown and go env checks |
| `frontendcov.go`, `gocov.go`, `appcov.go` | coverage inventories, profiles, the `wailsApps` table and the instrumented run of every app |
| `nativecov.go` | the `native coverage` step: clang source coverage + `llvm-cov` for own cgo C/Objective-C (task 069). A package with native code must have its `TestMain` call `internal/testutil/nativecov.Flush` before `os.Exit`, or the profile is empty and the step fails |
| `escapehatches.go` | the escape-hatch scan of TS and Go sources and tests |
| `isolation.go` | the `product isolation` step |
| `scripts.go` | the `scripts` step: shell scripts only hand over to `devtool` |
| `deps.go` | lockfile and build list, registry dates and integrity, audits, updates |
| `docs.go`, `plan.go`, `codexagents.go` | link and pointer checks and the `check-docs` command; the plan check, `check-plan` and `plan-nav`; Codex mirrors and the `codex-agents` command |
| `system.go` | `OSSystem`: the real file, process and HTTP calls |
