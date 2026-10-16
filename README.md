# OctopusTorrent

A personal local BitTorrent client for macOS (the owner's own Apple Silicon Mac): convenient adding of `.torrent` and magnet, selection of files and folders, pause and seeding, reliable restore after restart. No accounts, payments, cloud, analytics or built-in AI. **AI-first:** code and documentation are written so that a developer agent can continue the work using only this repository.

## Current state

**Stage P1 has started.** There are specifications, a mockup, a plan, a decision register, the **first minimal `.app`** (task 003): a Wails window with a React button that calls a Go method, **one local quality gate** (task 004): `go tool devtool check`, **the Go–TypeScript contract** (task 005): DTOs, errors, idempotent operations and ordering rules, executable in both languages, and **own test torrents with a closed local swarm** (task 006) on anacrolix/torrent v1.61.0. There are no product features (torrents, library) yet.

| What | State |
|---|---|
| Tasks [001](product/tasks/001-scope-and-decision-register.md), [002](product/tasks/002-repository-and-agent-entrypoint.md), [003](product/tasks/003-wails-macos-build-spike.md), [004](product/tasks/004-reproducible-check-commands.md), [005](product/tasks/005-domain-dto-and-bridge-contract.md), [006](product/tasks/006-controlled-torrent-fixtures.md) | see [navigation](product/tasks/README.md#navigation-of-all-tasks) |
| **Next tasks** | [007 — engine API and lifecycle](product/tasks/007-engine-api-lifecycle-spike.md), [008 — SQLite driver](product/tasks/008-sqlite-driver-durability-spike.md), [017 — local diagnostics](product/tasks/017-local-diagnostics-privacy.md), [035 — React bridge and resync](product/tasks/035-react-bridge-state-resync.md) |
| Coverage of our own code | 100% Go (statements, raw counters, unit tests + `.app` run) and 100% TS (4 metrics per file), enforced by `go tool devtool check` — [report 004](docs/verification/004-reproducible-check-commands.md), [report 005](docs/verification/005-domain-dto-and-bridge-contract.md), [report 006](docs/verification/006-controlled-torrent-fixtures.md) |

## Stack

React 19 + TypeScript 7 (strict) and Vite 8 · Wails v2.16.0 (system WebView) · Go 1.27.2 · [anacrolix/torrent](https://github.com/anacrolix/torrent) (version pinned by 007) · SQLite through a repository (driver — 008). Exact versions, the **update policy** (latest stable version at least 14 days old; Go/Node patch releases — immediately) and reasons — [toolchain-versions](docs/decisions/toolchain-versions.md); macOS target and bundle ID — [macos-target](docs/decisions/macos-target.md).

## Documents

| Document | Purpose |
|---|---|
| [AGENTS.md](AGENTS.md) | **Start here if you are an agent:** mandatory session rules and module boundaries. |
| [PROJECT_REQUIREMENTS.md](product/PROJECT_REQUIREMENTS.md) | Behavior and criteria: 80 requirement IDs, 19 end-to-end scenarios. The main source of truth. |
| [PRODUCT_CONCEPT.md](product/PRODUCT_CONCEPT.md) | Idea, personal limits, order of capabilities. |
| [APPLICATION_ARCHITECTURE.md](product/APPLICATION_ARCHITECTURE.md) | Components, DTOs, flows, SQLite, deletion, macOS. |
| [ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md](product/ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md) | Technical plan for the engine and Wails; assumptions that prototypes still have to prove. |
| [MOCKUP.html](product/MOCKUP.html) | Interactive mockup — a visual reference; demo toggles and demo logic are not the product ([discrepancies](docs/decisions/project-baseline.md#mockup-versus-specifications)). |
| [tasks/README.md](product/tasks/README.md) | Plan: 68 tasks, dependencies, requirement and scenario matrices, execution rules. |
| [docs/README.md](docs/README.md) | Index of engineering policy, decisions, evidence and feature documentation. |
| [docs/engineering/TDD_AND_QUALITY.md](docs/engineering/TDD_AND_QUALITY.md) | TDD, 100% coverage, no debt — mandatory for every change. |
| [docs/decisions/project-baseline.md](docs/decisions/project-baseline.md) | Agreed scope, requirement inventory, discrepancies, environment, open-decision register O1–O18. |
| [archive/](product/archive/LIBTORRENT_TECHNICAL_RESEARCH.md) | History of the rejected stack (libtorrent). Read-only, not a requirement. |
| [.claude/docs/README.md](.claude/docs/README.md) | Agent tooling: review agents, commands, action instructions shared with Codex, octopus and Conductor. |

## Directory map

Created now:

```text
README.md, AGENTS.md          entry point and agent rules
CLAUDE.md                     entry for Claude Code: imports AGENTS.md, commands, big picture
product/                      everything about the product concept and plan:
  PROJECT_REQUIREMENTS.md, PRODUCT_CONCEPT.md, APPLICATION_ARCHITECTURE.md,
  ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md, MOCKUP.html   specifications and mockup
  tasks/                      plan and 68 numbered tasks
  archive/                    historical materials, read-only
main.go, main_test.go         Wails entry point: embedded frontend/dist, run(start, stderr, ledger)
go.mod, go.sum                Go 1.27.2, Wails v2.16.0 (library + CLI as go tool), anacrolix/torrent v1.61.0 (tests only so far)
wails.json                    Wails configuration (npm ci, npm run build)
internal/app/                 Wails facade (AppAPI): GetRuntimeInfo, GetLibrarySnapshot, GetOperation, ErrorFormatter
internal/app/contracts/       bridge DTOs, AppError, strict requests, shared JSON examples (testdata/)
internal/app/operations/      idempotent operation ledger
internal/torrentengine/testutil/  fixture: own v1/v2/hybrid test torrents (testdata/); swarm: closed loopback swarm
cmd/devtool/, internal/devtool/   quality gate, plan and doc checks, workspace setup/run, run as go tool devtool
.claude/, .codex/             agents, commands, hook and action instructions for Claude Code and Codex
.octopus/, .conductor/        parallel-workspace runners: setup/run scripts, action prompts
frontend/src/bridge/          our own wrapper over generated bindings; contract/ — decoders, errors, ordering
frontend/src/features/        UI by feature (currently runtime-check)
frontend/src/test/            test setup
frontend/wailsjs/             bindings generated by Wails — do not edit by hand
frontend/package.json, package-lock.json, vite.config.ts, tsconfig.json
build/darwin/, build/appicon.png   Info.plist (bundle ID, minimum macOS), icon
docs/                         policy, ADRs, evidence, and docs by domain: app, bridge, frontend, tooling, development
```

**Not created yet** — from [architecture §3](product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first): `internal/torrentengine/` itself (only `testutil/` exists), `internal/library/`, `internal/files/`, `internal/automation/`, `internal/platform/`, `internal/diagnostics/`, `frontend/src/{state,i18n,styles}/`.

Tracing a scenario from the UI to Go: [desktop shell](docs/app/desktop-shell.md) — the button in `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. For a future requirement: [TR-01](product/PROJECT_REQUIREMENTS.md#22-downloading-and-seeding) → [matrix](product/tasks/README.md#full-coverage-of-the-80-requirements) → [025](product/tasks/025-pause-resume-application.md) → `internal/app` → `internal/torrentengine` / `internal/library`.

## Build and check commands

All commands run from the repository root and do not depend on an absolute path; verified on Apple
M4 / macOS 27.0.1 in tasks [003](docs/verification/003-wails-macos-build-spike.md) and
[004](docs/verification/004-reproducible-check-commands.md), including from a clean copy at another
path.

**Development prerequisites** (the finished `.app` does not need them): Xcode Command Line Tools
(`xcode-select --install`), Go ≥ 1.27 (`brew install go`; the exact 1.27.2 is pulled in by `go.mod`),
Node.js ≥ 22.12 with npm ≥ 11.20 (verified 26.11.1 / 11.20.0; `frontend/.npmrc` sets
`min-release-age=14`, so npm will not install a version younger than 14 days). A global Wails CLI is
**not needed**: `go tool wails`, `go tool govulncheck` and `go tool devtool` take their versions from
`go.mod`.

### The gate: one command for every check

```sh
go tool devtool check            # full gate; the only one that accepts a change (~25 s)
go tool devtool check offline    # without the network steps → at best GATE INCOMPLETE
go tool devtool check fast       # also without the .app run (TDD loop) → at best GATE INCOMPLETE
go tool devtool check deps       # only the network steps: release age, npm audit, govulncheck, updates
```

It runs, in order and never stopping early: `npm ci`, npm's effective cooldown, Go's effective module
settings (default proxy and checksum database, no exemptions), typecheck, lint
(Oxlint, type-aware), frontend tests with an **independent** 100% inventory check (all four metrics,
every own file by raw counts), frontend build, `gofmt`, `go vet`, regenerated Wails bindings
compared by content, `go test -race` with raw counters, an **instrumented `.app` run** (it opens an
OctopusTorrent window for a few seconds and quits it through an Apple Event, so `main()` is covered),
the merged Go profile with no zero block and a complete inventory, escape hatches (ignore directives,
lint disables, `@ts-` comments, `//nolint`, skipped or conditional tests), shell scripts that only
hand over to `devtool`, the release build,
documentation links, the task plan, the release age of every locked npm version and Go module (and
that each locked npm artifact is the registry's: `resolved` and `integrity`), `npm audit`, `govulncheck`, a report of updates out of
quarantine, and a check that no file changed during the run. Exit 0 only on `GATE PASSED` (or
`GATE INCOMPLETE` in a partial mode). Details: [quality gate](docs/tooling/quality-gate.md).

| The full gate needs | Why | Without it |
|---|---|---|
| Network | registry dates, `npm audit`, `govulncheck` | those steps are `not_run`: `check` fails, use `check offline` meanwhile |
| A logged-in GUI session on this Mac | the `.app` coverage run opens a window | `.app coverage run` fails or is `not_run` |
| Automation permission of the terminal for OctopusTorrent | quitting the `.app` with an Apple Event (macOS asks once) | `not_run`, naming System Settings → Privacy & Security → Automation |
| No other OctopusTorrent running | the run quits the app by bundle ID | `not_run: already running` (one workspace at a time) |

Not built yet and always printed as `not_run (not implemented yet)`: integration with the real
engine (tasks 007, 018) and UI tests through the test bridge (task 035). Manual QA of the real `.app`
(clicking through scenarios) stays with `/qa`.

### Quick check of one changed module (TDD, never acceptance)

```sh
go test -race ./internal/app -run TestGetRuntimeInfoReportsCompiledGoRuntime -v
go test -race -count=1 ./internal/torrentengine/...   # test torrents and the loopback swarm, about 12 s
npm --prefix frontend test -- src/features/runtime-check/RuntimeCheck.test.tsx
npm --prefix frontend run lint -- src/features/runtime-check/RuntimeCheck.tsx
```

### Individual commands

```sh
go tool wails doctor                          # environment check
go tool wails generate module                 # bindings after a Go facade change (committed)
go tool wails build -clean -trimpath          # release build → build/bin/OctopusTorrent.app (arm64, ad-hoc)
go tool wails dev                             # development with frontend hot reload
go tool devtool check-docs                    # documentation links and agent-instruction pointers
go tool devtool check-plan                    # the task plan; plan-nav regenerates its navigation table
go tool devtool plan-nav
go tool devtool codex-agents                  # Codex agents from .claude/agents
go test ./internal/torrentengine/testutil/fixture -args -update   # rewrite the committed test torrents
sh .octopus/scripts/setup.sh                  # workspace preparation for octopus / Conductor
OCTOPUS_PORT=34115 sh .octopus/scripts/run.sh # dev server for a workspace
```

Rules for scripts (AI-07): run from the root, paths relative to the root, no hidden steps; versions in
`go.mod`/`go.sum` and `frontend/package-lock.json`; generated bindings are reproduced by a command,
not edited; new checks go into `go tool devtool check` with tests, not into shell scripts.

## Working with the repository

Every task goes through RED → GREEN → REFACTOR, with 100% coverage of our own executable code, up-to-date feature documentation and no new debt. Order of actions — [tasks/README.md](product/tasks/README.md#how-to-execute-one-task) and the [workflow](docs/development/workflow.md), agent rules — [AGENTS.md](AGENTS.md); first time here — [getting started](docs/development/getting-started.md); every document — [docs/README.md](docs/README.md). Personal torrents, downloaded data and working databases are not committed (see [.gitignore](.gitignore)); synthetic test data lives in `testdata/`.
