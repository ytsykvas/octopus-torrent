# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The mandatory agent rules are in AGENTS.md; they apply in full:

@AGENTS.md

Everything written into the repository is English; replies to the owner are in Ukrainian ([decision](docs/decisions/repository-language.md)).

## Layout

- `product/` — everything about the product: [requirements](product/PROJECT_REQUIREMENTS.md) (source of behavior, requirement IDs), concept, [architecture](product/APPLICATION_ARCHITECTURE.md), engine research, `MOCKUP.html` (visual reference only — its demo logic is not a requirement), the plan and 68 numbered tasks in [product/tasks/](product/tasks/README.md#navigation-of-all-tasks), `archive/`.
- `docs/` — engineering policy, ADRs (`decisions/`, open questions O1–O18 in the [register](docs/decisions/project-baseline.md#6-open-decision-register)), evidence (`verification/`), feature docs (`features/`).
- Code: `main.go`, `internal/app` (Wails facade), `frontend/` (React), `cmd/devtool` + `internal/devtool` (agent and workspace tooling).
- Agent tooling: [.claude/docs/README.md](.claude/docs/README.md) — agents, commands, the action instructions shared with Codex, octopus and Conductor.

## Workflow

- Start a numbered task with `/task` (or `/task 004`): it reads the required context and plans in plan mode before any edit.
- `/check` runs every gate; `/branch-review` runs the three review agents; `/qa` drives the real `.app` through the `macos-qa` agent; `/pushit` commits, pushes to `origin` (`github.com/ytsykvas/octopus-torrent`) and opens a PR.
- Changes to the hook's own code, `go.mod`/`go.sum`, an `.npmrc` or agent settings need the owner: in the `auto`, `acceptEdits` or `bypassPermissions` permission modes the hook blocks them, so ask the owner to switch to `default` (Shift+Tab) first — task 004 is such a task.
- Adding or upgrading a dependency: [.claude/docs/dependencies.md](.claude/docs/dependencies.md). The `release-guard` hook blocks commands that would bypass the 14-day release cooldown; when it refuses, use the form it names — do not work around it.

## Commands

All commands run from the repository root. The full verified list and prerequisites are in [README → Commands](README.md#build-and-check-commands). There is no single "check everything" command yet; task [004](product/tasks/004-reproducible-check-commands.md) creates it — until then `/check` lists the steps.

```sh
npm --prefix frontend ci                      # .npmrc: min-release-age=14 — do not bypass
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage       # 100% threshold per file, 4 metrics
npm --prefix frontend run build               # required before go test: main.go embeds frontend/dist
gofmt -l . && go vet ./... && go test ./...
go tool wails generate module                 # after changing internal/app methods; frontend/wailsjs is committed
go tool wails build -clean -trimpath          # → build/bin/OctopusTorrent.app
go tool wails dev
go tool devtool check-docs                    # every relative link, anchor and .claude pointer resolves
go tool devtool codex-agents                  # regenerate .codex/agents after changing .claude/agents
```

A single test:

```sh
go test ./internal/app -run TestGetRuntimeInfoReportsCompiledGoRuntime -v
npm --prefix frontend test -- src/features/runtime-check/RuntimeCheck.test.tsx -t 'Go'
```

For a single file use `test`, not `test:coverage`: the `perFile` threshold fails on files that this run did not execute.

The Wails CLI and `devtool` run only through `go tool` (both are `tool` directives in `go.mod`); do not install a global `wails`.

## Go coverage

`main()` runs only in the real application, so 100% for Go is the merged profile of the unit tests and the instrumented `.app` (procedure in the [README](README.md#build-and-check-commands)). The `cmd/devtool` test writes its instrumented binary's counters into the same unit directory. The criterion is empty output from `awk 'NR>1 && $NF==0' build/coverage/go.out`, not the rounded `100.0%` label. After an instrumented build, make a regular release build.

For TS, the sources are defined by `vite.config.ts` (`include: src/**/*.{ts,tsx}`). `frontend/wailsjs/` lies outside `src/` and is not own code; own wrappers over it live in `frontend/src/bridge` and are measured.

## Big picture

- Only the shell from task 003 is implemented: `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. This chain is the template for every new UI → Go command.
- `main.go` keeps `main()` free of branches: `run(start, stderr)` takes the Wails start function, so tests substitute a fake instead of a window. New facade dependencies are passed explicitly through `app.New(...)` and `options.App.Bind`. `cmd/devtool` follows the same pattern with the `devtool.System` interface.
- React components receive their bridge dependencies as props (see `RuntimeCheck`), so they are tested without Wails. `App.tsx` wires them to the real bridge.
- The target structure (`internal/torrentengine`, `internal/library`, `internal/files`, `internal/platform`, `frontend/src/state`, etc.) is in [architecture §3](product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first). Those directories are created by numbered tasks, not invented in advance.

## Environment specifics

- One verified environment: Apple M4, macOS 27.0.1 arm64 ([macos-target](docs/decisions/macos-target.md)). Bundle ID `dev.tsykvas.octopustorrent`; to quit the app from a script: `osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'` (returns -128, but the app quits).
- The linker warning about macOS 13.0 versus 11.0 is harmless ([toolchain-versions](docs/decisions/toolchain-versions.md#consequences)).
- Never `Read` a screenshot in a long session; let `macos-qa` read window text through Accessibility.
- Check Markdown links only in Git files (`git ls-files --cached --others --exclude-standard '*.md'`); otherwise the check descends into `frontend/node_modules`.
