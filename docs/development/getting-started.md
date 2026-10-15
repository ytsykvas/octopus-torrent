# Getting started

> Last updated: 2026-10-15

From a fresh clone to a running window and a passing gate on an Apple Silicon Mac. The full command
list is in the [README](../../README.md#build-and-check-commands); this page is the order to run
them in and what to do when something goes wrong.

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| macOS | 27.0+ on Apple Silicon | the only verified environment ([macos-target](../decisions/macos-target.md)) |
| Xcode Command Line Tools | current | `xcode-select --install` |
| Go | ≥ 1.27 | `go.mod` pulls the exact toolchain (1.27.2) |
| Node.js / npm | ≥ 22.12 / ≥ 11.20 | verified 26.11.1 / 11.20.0 |

There is no global Wails CLI: `go tool wails`, `go tool govulncheck` and `go tool devtool` take
their versions from `go.mod`.

## First setup

```sh
npm --prefix frontend ci              # exact locked versions; .npmrc refuses packages < 14 days old
npm --prefix frontend run build       # frontend/dist — main.go embeds it, go test needs it
go mod download
go tool wails doctor                  # environment check
```

`sh .octopus/scripts/setup.sh` builds `build/bin/devtool` and runs the first three steps (not
`wails doctor`); it is what octopus and Conductor run.

## Daily loop

```sh
go tool wails dev                     # window + Vite hot reload; browser at http://localhost:34115
go tool devtool check fast            # TDD loop: no .app run, no network → GATE INCOMPLETE at best
go tool devtool check                 # the gate: only GATE PASSED accepts a change
```

To check one module quickly (never acceptance):

```sh
go test -race ./internal/app/operations -run TestRepeatedOperationIDWithTheSameRequestRunsOnce -v
npm --prefix frontend test -- src/bridge/contract/ordering.test.ts
npm --prefix frontend run lint -- src/bridge/contract/ordering.ts
```

For a single TypeScript file use `test`, not `test:coverage`: the per-file threshold fails on files
that run did not execute.

## Build and open the app

```sh
go tool wails build -clean -trimpath  # → build/bin/OctopusTorrent.app (arm64, ad-hoc signed)
open build/bin/OctopusTorrent.app
osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'   # returns -128; the app quits
```

The linker warning about macOS 13.0 versus 11.0 is harmless
([toolchain-versions](../decisions/toolchain-versions.md#consequences)).

## Several workspaces

octopus and Conductor run `sh .octopus/scripts/run.sh`. It runs `wails dev` on `OCTOPUS_PORT` or
`CONDUCTOR_PORT`, so each workspace gets its own port. The gate's `.app` step refuses
(`not_run: already running`) while any app with bundle ID `dev.tsykvas.octopustorrent` runs —
another gate run or, very likely, a `wails dev` window, which uses the same ID. Quit them first. See
[agent tooling](../tooling/agent-tooling.md).

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `go test` fails: `pattern all:frontend/dist: no matching files` | `frontend/dist` is missing: `npm --prefix frontend run build` |
| `git status` shows `frontend/wailsjs/runtime/*` modified after `wails dev`/`generate` | Wails flips file modes: `git checkout -- frontend/wailsjs/runtime` (the gate compares content only) |
| Gate: `.app coverage run` `not_run` naming Automation | Allow your terminal in System Settings → Privacy & Security → Automation → OctopusTorrent |
| Gate: `.app coverage run` `not_run: already running` | Quit the other OctopusTorrent (another workspace) and rerun |
| Gate: `single code version` `FAIL` | Something edited files during the run; rerun |
| Gate hangs | A tool waits: an unanswered Automation prompt or a stalled `npm ci`. Answer it, or interrupt and rerun ([quality gate](../tooling/quality-gate.md#usage-and-recovery)) |
| `npm ci` refuses a version | It is younger than 14 days. Do not lower `min-release-age`; pick an older version ([dependencies.md](../../.claude/docs/dependencies.md)) |
| A call from the dev browser never settles | Wails could not parse its arguments; the dev server drops that error ([facade](../app/facade.md#errors)) |
| Ctrl-C during the gate left an instrumented app running | Quit it with the `osascript` line above, then rerun the gate (it rebuilds the release `.app`) |
