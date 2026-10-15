# OctopusTorrent

A personal local BitTorrent client for macOS (the owner's own Apple Silicon Mac): convenient adding of `.torrent` and magnet, selection of files and folders, pause and seeding, reliable restore after restart. No accounts, payments, cloud, analytics or built-in AI. **AI-first:** code and documentation are written so that a developer agent can continue the work using only this repository.

## Current state

**Stage P1 has started.** There are specifications, a mockup, a plan, a decision register and the **first minimal `.app`** (task 003): a Wails window with a React button that calls a Go method. There are no product features (torrents, library) yet.

| What | State |
|---|---|
| Tasks [001](product/tasks/001-scope-and-decision-register.md), [002](product/tasks/002-repository-and-agent-entrypoint.md), [003](product/tasks/003-wails-macos-build-spike.md) | see [navigation](product/tasks/README.md#navigation-of-all-tasks) |
| **Next tasks** | [004 — single check command](product/tasks/004-reproducible-check-commands.md), [005 — DTO and bridge contract](product/tasks/005-domain-dto-and-bridge-contract.md) |
| Coverage of our own code | 100% Go (statements, raw counters) and 100% TS (4 metrics per file) — [report 003](docs/verification/003-wails-macos-build-spike.md) |

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
main.go, main_test.go         Wails entry point: embedded frontend/dist, run(start, stderr)
go.mod, go.sum                Go 1.27.2, Wails v2.16.0 (library + CLI as go tool)
wails.json                    Wails configuration (npm ci, npm run build)
internal/app/                 Wails facade (AppAPI): GetRuntimeInfo
cmd/devtool/, internal/devtool/   agent hook (release-guard) and workspace setup/run, run as go tool devtool
.claude/, .codex/             agents, commands, hook and action instructions for Claude Code and Codex
.octopus/, .conductor/        parallel-workspace runners: setup/run scripts, action prompts
frontend/src/bridge/          our own wrapper over generated bindings
frontend/src/features/        UI by feature (currently runtime-check)
frontend/src/test/            test setup
frontend/wailsjs/             bindings generated by Wails — do not edit by hand
frontend/package.json, package-lock.json, vite.config.ts, tsconfig.json
build/darwin/, build/appicon.png   Info.plist (bundle ID, minimum macOS), icon
docs/                         policy, decisions (ADR), evidence, features
```

**Not created yet** — from [architecture §3](product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first): `internal/torrentengine/`, `internal/library/`, `internal/files/`, `internal/automation/`, `internal/platform/`, `internal/diagnostics/`, `frontend/src/{state,i18n,styles}/`, `scripts/`.

Tracing a scenario from the UI to Go: [desktop-shell-build](docs/features/desktop-shell-build.md) — the button in `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. For a future requirement: [TR-01](product/PROJECT_REQUIREMENTS.md#22-downloading-and-seeding) → [matrix](product/tasks/README.md#full-coverage-of-the-80-requirements) → [025](product/tasks/025-pause-resume-application.md) → `internal/app` → `internal/torrentengine` / `internal/library`.

## Build and check commands

Verified in task 003 on Apple M4 / macOS 27.0.1 ([report](docs/verification/003-wails-macos-build-spike.md)). All commands run from the repository root and do not depend on an absolute path. A single command for all checks will be created by [004](product/tasks/004-reproducible-check-commands.md).

**Development prerequisites** (the finished `.app` does not need them): Xcode Command Line Tools (`xcode-select --install`), Go ≥ 1.27 (`brew install go`; the exact 1.27.2 is pulled in by `go.mod`), Node.js ≥ 22.12 with npm ≥ 11.20 (verified 26.11.1 / 11.20.0; `frontend/.npmrc` sets `min-release-age=14`, so npm will not install a version younger than 14 days). A global Wails CLI is **not needed**: `go tool wails` takes the version from `go.mod`.

```sh
go tool wails doctor                          # environment check

# Frontend: dependencies, types, tests with a 100% per-file threshold, build
npm --prefix frontend ci
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage
npm --prefix frontend run build               # required before go test: main embeds frontend/dist

# Go: formatting, analyzer, tests
gofmt -l .                                    # must be empty
go vet ./...
go test ./...

# Bindings after a Go facade change (the result is committed)
go tool wails generate module

# Release build → build/bin/OctopusTorrent.app (arm64, ad-hoc signature)
go tool wails build -clean -trimpath

# Development with frontend hot reload
go tool wails dev

# Documentation links and agent-instruction pointers; Codex agents from .claude/agents
go tool devtool check-docs
go tool devtool codex-agents

# Task plan: verify it, and regenerate the navigation table after changing a task file
go tool devtool check-plan
go tool devtool plan-nav

# Workspace preparation and dev server for octopus / Conductor (logic in cmd/devtool)
sh .octopus/scripts/setup.sh
OCTOPUS_PORT=34115 sh .octopus/scripts/run.sh
```

**100% Go coverage** is measured together with a real `.app` launch, because `main()` runs only in the real application:

```sh
C="$PWD/build/coverage"; rm -rf "$C"; mkdir -p "$C/go-unit" "$C/go-app"
go test -cover -coverpkg=./... ./... -args -test.gocoverdir="$C/go-unit"   # includes the devtool binary
GOFLAGS="-cover -coverpkg=./..." go tool wails build -clean -trimpath
open -n --env GOCOVERDIR="$C/go-app" build/bin/OctopusTorrent.app
# click "Check Go connection", then quit the application (⌘Q)
go tool covdata textfmt -i="$C/go-unit,$C/go-app" -o "$C/go.out"
awk 'NR>1 && $NF==0' "$C/go.out"              # must be empty: no uncovered block
go tool wails build -clean -trimpath          # restore the normal release build
```

Rules for future scripts (AI-07): run from the root, paths relative to the root, no hidden steps; versions in `go.mod`/`go.sum` and `frontend/package-lock.json`; generated bindings are reproduced by a command, not edited.

## Working with the repository

Every task goes through RED → GREEN → REFACTOR, with 100% coverage of our own executable code, up-to-date feature documentation and no new debt. Order of actions — [tasks/README.md](product/tasks/README.md#how-to-execute-one-task), agent rules — [AGENTS.md](AGENTS.md). Personal torrents, downloaded data and working databases are not committed (see [.gitignore](.gitignore)); synthetic test data lives in `testdata/`.
