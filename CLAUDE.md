# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The mandatory agent rules are in AGENTS.md; they apply in full:

@AGENTS.md

Everything written into the repository is English; replies to the owner are in Ukrainian ([decision](docs/decisions/repository-language.md)).

## Layout

- `product/` — everything about the product: [requirements](product/PROJECT_REQUIREMENTS.md) (source of behavior, requirement IDs), concept, [architecture](product/APPLICATION_ARCHITECTURE.md), engine research, `MOCKUP.html` (visual reference only — its demo logic is not a requirement), the plan and 68 numbered tasks in [product/tasks/](product/tasks/README.md#navigation-of-all-tasks), `archive/`.
- `docs/` — engineering policy, ADRs (`decisions/`, open questions O1–O18 in the [register](docs/decisions/project-baseline.md#6-open-decision-register)), evidence (`verification/`), and how-it-works docs by domain: `app/`, `bridge/`, `frontend/`, `tooling/`, `development/` (index: [docs/README.md](docs/README.md); start with [getting started](docs/development/getting-started.md)).
- Code: `main.go`, `internal/app` (Wails facade), `frontend/` (React), `cmd/devtool` + `internal/devtool` (agent and workspace tooling), `internal/torrentengine/testutil` (own test torrents and a closed loopback swarm for engine tests), `internal/torrentengine` (engine contract tests until the 018 adapter), the temporary `EngineSpike.app` (`cmd/engine-spike`, `internal/enginespike`, `frontend/src/spike`; deleted by 034).
- Agent tooling: [.claude/docs/README.md](.claude/docs/README.md) — agents, commands, the action instructions shared with Codex, octopus and Conductor.

## Workflow

- Start a numbered task with `/task` (or `/task 004`): it reads the required context and plans in plan mode before any edit.
- `/check` runs `go tool devtool check` (the single gate); `/branch-review` runs the three review agents; `/qa` drives the real `.app` through the `macos-qa` agent; `/pushit` commits, pushes to `origin` (`github.com/ytsykvas/octopus-torrent`) and opens a PR.
- Changes to an `.npmrc` need the owner.
- Adding or upgrading a dependency: [.claude/docs/dependencies.md](.claude/docs/dependencies.md). No hook refuses commands any more ([ADR](docs/decisions/agent-tooling.md#no-pre-tool-hook)): the 14-day cooldown is kept by `frontend/.npmrc`, the gate and these rules — exact versions only, no `npx` download, no global install, no `@latest`.

## Commands

All commands run from the repository root. The full verified list, prerequisites and what the gate needs (network, GUI session, Automation permission) are in [README → Commands](README.md#build-and-check-commands).

```sh
go tool devtool check                         # THE gate: every check, never stops early; only it accepts a change
go tool devtool check fast                    # TDD loop: no .app run, no network → GATE INCOMPLETE at best
go tool devtool check offline                 # no network steps → GATE INCOMPLETE at best
go tool devtool check deps                    # release age, npm audit, govulncheck, updates out of quarantine
npm --prefix frontend ci                      # .npmrc: min-release-age=14 — do not bypass
npm --prefix frontend run build               # required before go test: main.go embeds frontend/dist
go tool wails generate module                 # after changing internal/app methods; frontend/wailsjs is committed
go tool wails build -clean -trimpath          # → build/bin/OctopusTorrent.app
go tool wails dev
go tool devtool check-docs                    # every relative link, anchor and .claude pointer resolves
go tool devtool check-plan                    # task plan: links, acyclic graph, statuses, coverage, navigation
go tool devtool plan-nav                      # regenerate the navigation table after changing a task file
go tool devtool codex-agents                  # regenerate .codex/agents after changing .claude/agents
```

A single test (quick module check, never acceptance):

```sh
go test -race ./internal/app -run TestGetRuntimeInfoReportsCompiledGoRuntime -v
go test -race -count=1 ./internal/torrentengine/...   # test torrents + loopback swarm + engine contract tests (anacrolix), about 15 s
npm --prefix frontend test -- src/features/runtime-check/RuntimeCheck.test.tsx -t 'Go'
npm --prefix frontend run lint -- src/features/runtime-check/RuntimeCheck.tsx
```

For a single file use `test`, not `test:coverage`: the `perFile` threshold fails on files that this run did not execute.

The Wails CLI, `govulncheck` and `devtool` run only through `go tool` (`tool` directives in `go.mod`); do not install a global `wails`.

## Coverage

`go tool devtool check` proves 100% itself; do not reconstruct it by hand. Go: `go test -race -covermode=atomic` writes raw counters, then the gate builds an instrumented `.app`, launches it with `GOCOVERDIR` and quits it with an Apple Event (a normal exit writes the counters; SIGTERM would not), because `main()` runs only in the real application. Both profiles must come from the same sources; every own file with a function must be measured; no block may be zero. `check fast` reports the `main()` blocks as `not_run`, never as covered. The `cmd/devtool` test writes its instrumented binary's counters into the same unit directory.

TS: the gate derives the inventory from the repository listing (`frontend/src/**/*.ts(x)` except tests, `src/test/` and `.d.ts`), not from `vite.config.ts`, and requires covered == total on all four metrics for every file. `frontend/wailsjs/` lies outside `src/` and is not own code (it is checked by regeneration); own wrappers over it live in `frontend/src/bridge` and are measured. Ignore directives, lint disables and skipped tests fail the gate.

## Big picture

- More than one Wails app: every project is a row of `wailsApps` (`internal/devtool/appcov.go`); the gate builds, runs and checks the bindings of each, and the `product isolation` step keeps the spike, `testutil` and (until 018) anacrolix out of the product binary.
- Implemented: the shell from task 003 (`frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`) and the contract from task 005 ([ADR api-contract](docs/decisions/api-contract.md)).
- The template for every new UI → Go command: a request DTO in `internal/app/contracts` with JSON examples in its `testdata/manifest.json`, `Validate()`, a facade method that starts the mutation through `internal/app/operations.Ledger` (idempotent `operationID`), regenerated bindings, a wrapper in `frontend/src/bridge` built on `callGo` + a decoder from `bridge/contract`, returning `Result<T>`. 64-bit values are decimal strings with `ts_type:"string"` in Go and `bigint` in TS; errors are `AppError`, sent as JSON text by `app.ErrorFormatter`; enumerations and error codes are listed once in `internal/app/contracts/testdata/vocabulary.json`, checked by both test suites.
- `main.go` keeps `main()` free of branches: `run(start, stderr, ledger)` takes the Wails start function, so tests substitute a fake instead of a window; `OnShutdown` stops the ledger. New facade dependencies are passed explicitly through `app.New(...)` and `options.App.Bind`; lifecycle hooks never become exported `App` methods (Wails binds them all to JavaScript). `cmd/devtool` follows the same pattern with the `devtool.System` interface.
- Engine tests (task 006, [ADR controlled-swarm](docs/decisions/controlled-swarm.md)) use `internal/torrentengine/testutil/fixture` (deterministic v1/v2/hybrid torrents committed in `testdata/`, rewritten with `go test ./internal/torrentengine/testutil/fixture -args -update`) and `.../swarm` (anacrolix clients on 127.0.0.1 only, every discovery path off, every wait bounded by a context). anacrolix/torrent v1.61.0 has tracked defects: v2-only torrents panic on add, the default mmap IO fails a chunk spanning a zero-length file, `Client.Close` closes listeners asynchronously, and `VerifyDataContext` returns before the completion is stored. Task 007 ([ADR engine-lifecycle](docs/decisions/engine-lifecycle.md)) confirmed the pin and adds: `VerifyDataContext` after `Drop` hangs the client, the `AddTorrentOpts` upload/download fields are ignored, info bytes that do not match a v1 infohash are accepted (also from a peer for a magnet), allowing download right after disallowing it stalls the transfer (the upload switch too, rarely, under load), and a failed piece check renames existing files to `<name>.part`, which a download then overwrites. Ownership: only the `new=true` import may `Drop`; only the client's creator closes it.
- React components receive their bridge dependencies as props (see `RuntimeCheck`), so they are tested without Wails. `App.tsx` wires them to the real bridge.
- The target structure (`internal/torrentengine`, `internal/library`, `internal/files`, `internal/platform`, `frontend/src/state`, etc.) is in [architecture §3](product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first). Those directories are created by numbered tasks, not invented in advance.

## Environment specifics

- One verified environment: Apple M4, macOS 27.0.1 arm64 ([macos-target](docs/decisions/macos-target.md)). Bundle ID `dev.tsykvas.octopustorrent`; to quit the app from a script: `osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'` (returns -128, but the app quits).
- The linker warning about macOS 13.0 versus 11.0 is harmless ([toolchain-versions](docs/decisions/toolchain-versions.md#consequences)).
- Never `Read` a screenshot in a long session; let `macos-qa` read window text through Accessibility.
- Check Markdown links only in Git files (`git ls-files --cached --others --exclude-standard '*.md'`); otherwise the check descends into `frontend/node_modules`.
