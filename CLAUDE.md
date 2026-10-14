# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The mandatory agent rules are in AGENTS.md; they apply in full:

@AGENTS.md

Everything written into the repository is English; replies to the owner are in Ukrainian ([decision](docs/decisions/repository-language.md)).

## Commands

All commands run from the repository root. The full verified list and prerequisites are in [README → Commands](README.md#build-and-check-commands). There is no single "check everything" command yet; task [004](tasks/004-reproducible-check-commands.md) creates it.

```sh
npm --prefix frontend ci                      # .npmrc: min-release-age=14 — do not bypass
npm --prefix frontend run typecheck
npm --prefix frontend run test:coverage       # 100% threshold per file, 4 metrics
npm --prefix frontend run build               # required before go test: main.go embeds frontend/dist
gofmt -l . && go vet ./... && go test ./...
go tool wails generate module                 # after changing internal/app methods; frontend/wailsjs is committed
go tool wails build -clean -trimpath          # → build/bin/OctopusTorrent.app
go tool wails dev
```

A single test:

```sh
go test ./internal/app -run TestGetRuntimeInfoReportsCompiledGoRuntime -v
npm --prefix frontend test -- src/features/runtime-check/RuntimeCheck.test.tsx -t 'Go'
```

For a single file use `test`, not `test:coverage`: the `perFile` threshold will fail on files that this run did not execute.

The Wails CLI is used only through `go tool wails` (the version is pinned in `go.mod` with the `tool` directive); do not install a global `wails`.

## Go coverage

`main()` runs only in the real application, so 100% for Go is the merged profile of the unit tests and the instrumented `.app` (procedure in the [README](README.md#build-and-check-commands)). The criterion is empty output from `awk 'NR>1 && $NF==0' build/coverage/go.out`, not the rounded `100.0%` label. After an instrumented build, make a regular release build.

For TS, the sources are defined by `vite.config.ts` (`include: src/**/*.{ts,tsx}`). `frontend/wailsjs/` lies outside `src/` and is not own code; own wrappers over it live in `frontend/src/bridge` and are measured.

## Big picture

- Currently only the shell from task 003 is implemented: `frontend/src/features/runtime-check` → `frontend/src/bridge/runtimeInfo.ts` → `frontend/wailsjs/go/app/App.js` → `internal/app.App.GetRuntimeInfo`. This same chain is the template for every new UI → Go command.
- `main.go` keeps `main()` free of branches: `run(start, stderr)` takes the Wails start function, so tests substitute a fake instead of a window. New facade dependencies are passed explicitly through `app.New(...)` and `options.App.Bind`.
- React components receive their bridge dependencies as props (see `RuntimeCheck`), so they are tested without Wails. `App.tsx` wires them to the real bridge.
- The target structure (`internal/torrentengine`, `internal/library`, `internal/files`, `internal/platform`, `frontend/src/state`, etc.) is described in [architecture §3](APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first). Most of these directories do not exist yet. They are created by numbered tasks, not invented in advance.
- The source of behavior is [PROJECT_REQUIREMENTS.md](PROJECT_REQUIREMENTS.md) with requirement IDs. What to do next is in [tasks/README.md](tasks/README.md#navigation-of-all-tasks). Open questions O1–O18 are in the [decision register](docs/decisions/project-baseline.md#6-open-decision-register). `MOCKUP.html` is only a visual reference; its demo logic is not a requirement.

## Environment specifics

- There is one verified environment: Apple M4, macOS 27.0.1 arm64 ([macos-target](docs/decisions/macos-target.md)). The bundle ID is `dev.tsykvas.octopustorrent`; to quit the application from a script: `osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'` (returns -128, but the application quits).
- The linker warning about macOS 13.0 versus 11.0 is harmless ([toolchain-versions](docs/decisions/toolchain-versions.md#consequences)).
- Check Markdown links only in Git files (`git ls-files --cached --others --exclude-standard '*.md'`), because otherwise the check descends into `frontend/node_modules`.
