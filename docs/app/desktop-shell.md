# Desktop shell, `.app` build and the first React → Go connection

**Status:** `implemented` (prototype from task 003; the product UI will replace it in 034/035/040)  
**Requirements:** REL-01, REL-02, STACK-05, UI-04 (partially), AI-07, QA-01, QA-02, QA-05 (partially)  
**Related tasks:** [003](../../product/tasks/003-wails-macos-build-spike.md); next — [004](../../product/tasks/004-reproducible-check-commands.md) (single check command), [034](../../product/tasks/034-wails-facade-real-bindings.md) (real facade), [035](../../product/tasks/035-react-bridge-state-resync.md) (bridge and state)  
**Last verified version:** refactor after 009 (runtime info as a `Result`); task 005 (startup and quit wiring); task 003 (window and check UI)  
**Last updated:** 2026-10-18

## Goal and behavior

The owner opens `OctopusTorrent.app` from Finder on their Mac. The Wails window (system WebView) shows a local React UI with a **Check Go connection** button. Clicking it calls a Go method through the generated bindings and shows the response `Go go1.27.2 · darwin/arm64 · SQLite 3.53.4`; the SQLite part, added by [008](../../product/tasks/008-sqlite-driver-durability-spike.md), is the version compiled into the binary. A failed or malformed answer is shown as `role="alert"` with the bridge's English fallback for its error code (for example `The application did not answer.`); diagnostic text is never shown. The UI is embedded in the binary and works without internet; the finished package does not need Go, Node.js or the Wails CLI.

Only this proof of the path is implemented. The torrent list, themes, i18n and the rest are in later tasks (prototype texts are in English; localization is added by 041).

## Startup and quit

```
Finder / `open` → OctopusTorrent.app/Contents/MacOS/OctopusTorrent
  main() → run(wails.Run, os.Stderr, operations.NewLedger(contracts.NewRuntimeID()))
    wails.Run(options.App{ embedded frontend/dist, Bind: app.New(ledger),
                           ErrorFormatter: app.ErrorFormatter(stderr), OnShutdown: stopOperations })
      window (WKWebView) loads index.html → src/main.tsx → <App/>
  Cmd+Q / Apple Event quit → OnShutdown → ledger.Shutdown (≤ 5 s) → process exits 0
  wails.Run error (no WebView, …) → "OctopusTorrent: <error>" on stderr, exit 1
```

- Each start makes a new `runtimeID`; the frontend uses it to tell a restart from a continuing run
  ([ordering](../bridge/ordering.md)).
- Quit from a script: `osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'`.
  It returns -128, but the app quits. A normal quit, unlike SIGTERM, also writes coverage counters
  of an instrumented build ([quality gate](../tooling/quality-gate.md)).
- `build/darwin/Info.plist` sets bundle ID `dev.tsykvas.octopustorrent` and minimum macOS 27.0
  ([macos-target](../decisions/macos-target.md)). `Info.dev.plist` adds only
  `NSAllowsLocalNetworking` for `wails dev`.

## Architecture analysis before the change

- The first code in the repository; nothing to reuse. Boundaries are taken from [architecture §2–§3](../../product/APPLICATION_ARCHITECTURE.md#2-components-and-dependency-directions): facade in `internal/app`, own bridge in `frontend/src/bridge`, UI in `frontend/src/features`.
- There is no state or side effects in Go; `GetRuntimeInfo` returns the embedded runtime and the SQLite version that `run` read once at start from an in-memory database (`library.SQLiteVersion`; no file, no data directory). If that read fails, the application does not start: `run` writes the reason to stderr and returns 1. The Wails context (`OnStartup`) is not needed, so it was not added.
- The `main` entry point is reduced to a single line without branches; all logic is in `run(start, stderr)` with an explicit `start` dependency (`wails.Run` in the product), which makes it testable without a window. Task 005 added the operation ledger (which owns the runtime ID) as a further explicit parameter ([bridge-contract](../bridge/contract.md)).
- The React component receives `loadRuntimeInfo` as a prop; only `App` wires up the real bridge. Tests work with a fake function, without Wails.

## Contracts and data

| Go (`internal/app`) | JSON / TS (`frontend/src/bridge/runtimeInfo.ts`) |
|---|---|
| `func (*App) GetRuntimeInfo() RuntimeInfo` | `getRuntimeInfo(): Promise<Result<RuntimeInfo>>` |
| `RuntimeInfo{GoVersion, OS, Arch, SQLiteVersion string}` | `{ goVersion: string; os: string; arch: string; sqliteVersion: string }` |

The wrapper follows the bridge template ([calling Go](../bridge/calling-go.md)): `callGo` decodes the answer with the `runtimeInfo` decoder (`bridge/contract/runtimeInfo.ts`) into a plain object, never the generated `app.RuntimeInfo` class, and returns a `Result`. It never rejects: a rejection becomes an `AppError`, a missing or extra field `contract.invalidDto`. The component shows `describeError(error).fallback`.

## Scenarios and TDD

| Scenario | Test | RED (before implementation) |
|---|---|---|
| Facade returns embedded runtime values; contract JSON keys | `internal/app/app_test.go` | `undefined: New`, `undefined: RuntimeInfo` |
| `run` passes `wails.Run` the name, the embedded `frontend/dist/index.html`, exactly one bind `*app.App`; startup error → stderr and code 1 | `main_test.go` | `undefined: run` |
| `OnShutdown` stops the ledger; a mutation that ignores cancellation is reported on stderr (task 005) | `main_test.go`: `TestRunStopsOperationsOnShutdown`, `TestStopOperationsReportsMutationsThatDidNotStop` | `run` signature |
| Bridge: success → decoded DTO; a malformed answer → `contract.invalidDto`; a rejection → `bridge.callFailed` (same values as the Go JSON pin) | `src/bridge/runtimeInfo.test.ts` | `Failed to resolve import "./runtimeInfo"`; the `Result` form (refactor after 009): three failures, the wrapper still rejected and passed a malformed answer through |
| UI: initial state, "Checking…", success, an error `Result` shown by its fallback without diagnostic text, keyboard (Tab + Enter) and focus | `src/features/runtime-check/RuntimeCheck.test.tsx` | `Failed to resolve import "./RuntimeCheck"` |
| `App` wires up the bridge; heading | `src/App.test.tsx` | `Failed to resolve import "./App"` |
| `main.tsx` mounts the app into `#root`; without `#root` — a clear error | `src/main.test.tsx` | `Failed to resolve import "./main"` |
| Real `.app`: click → Go response; launch from Finder; repeated launch; offline | manual + System Events | see [report 003](../verification/003-wails-macos-build-spike.md) |

## Implementation and reuse

```text
main.go                         embed frontend/dist; run(start, stderr, ledger); stopOperations; main()
internal/app/app.go             App facade (see facade.md), RuntimeInfo DTO, ErrorFormatter
frontend/src/main.tsx           mounting into #root
frontend/src/App.tsx            composition: RuntimeCheck + real bridge
frontend/src/bridge/runtimeInfo.ts       the single call of the generated GetRuntimeInfo (callGo + runtimeInfo decoder)
frontend/src/features/runtime-check/     connection check UI
frontend/wailsjs/               generated by `go tool wails generate module` — do not edit
build/darwin/Info*.plist        bundle ID, minimum macOS (ADR macos-target)
wails.json                      npm ci, npm run build
```

## Checks and 100% coverage

Commands are in the [README](../../README.md#build-and-check-commands); actual results are in [report 003](../verification/003-wails-macos-build-spike.md).

- **Go:** unit profile (`-test.gocoverdir`) + profile of an instrumented real `.app` run (`GOFLAGS=-cover`), merged with `go tool covdata textfmt`. Inventory: `main.go`, `internal/app/app.go`. Blocks with a zero counter: 0; `covdata percent`: 100.0% for both packages. Unit tests alone are not enough: they give 83.3% for `main` (the `main()` line is uncovered), so the instrumented run is mandatory.
- **TypeScript:** Vitest + v8, `include: src/**/*.{ts,tsx}`, threshold 100 for lines/statements/functions/branches **per file**. 4 files, 19/19 statements, 10/10 branches, 6/6 functions. Negative check: a temporary unimported file without tests fails the threshold.
- Not counted as own code: `frontend/wailsjs/` (reproducible generated), `src/**/*.test.*`, `src/test/setup.ts` (test infrastructure).
- Analyzers: `gofmt -l` empty, `go vet` clean, `tsc --noEmit` (strict, `noUncheckedIndexedAccess`, `noUnused*`) clean. ESLint and the centralized command — 004.

## Refactoring and debt check

- The Wails template logic (`Greet`, an unneeded `OnStartup`, fonts, logo, CSS) was not carried over.
- `main` was simplified to a call without branches so that the instrumented run covers it fully.
- Found and fixed: absolute developer paths in the binary (`-trimpath`); the Wails cache `frontend/package.json.md5` added to `.gitignore`.
- No temporary workarounds or TODOs. The prototype UI is an agreed interim feature until 034/035/040, not debt.

## Usage and recovery

1. Install Go (`brew install go`), have Node.js ≥ 22.12 and Xcode Command Line Tools.
2. `go tool wails build -clean -trimpath` → `build/bin/OctopusTorrent.app`.
3. Open from Finder. If macOS is older than 27.0, launch is refused (see ADR).

## Change history and evidence

| Date | Change | Evidence |
|---|---|---|
| 2026-10-15 | Moved from `docs/features/desktop-shell-build.md`; startup and quit section; `run` takes the operation ledger, `OnShutdown` stops it, errors cross as AppError JSON (task 005) | [report 005](../verification/005-domain-dto-and-bridge-contract.md) |
| 2026-10-17 | The runtime check also shows the bundled SQLite version | [report 008](../verification/008-sqlite-driver-durability-spike.md), ADR [sqlite-driver](../decisions/sqlite-driver.md) |
| 2026-10-14 | First `.app`, `GetRuntimeInfo` facade, bridge, check UI, 100% Go/TS coverage | [report 003](../verification/003-wails-macos-build-spike.md), ADR [toolchain-versions](../decisions/toolchain-versions.md), [macos-target](../decisions/macos-target.md) |
