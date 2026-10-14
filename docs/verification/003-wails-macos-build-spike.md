# Report 003: the first Wails `.app` on the owner's own Mac

**Task:** [003](../../tasks/003-wails-macos-build-spike.md)  
**Date:** 14–15 October 2026  
**Change version:** the task 003 commit (parent `248079e`)  
**Feature:** [desktop-shell-build](../features/desktop-shell-build.md); **ADR:** [toolchain-versions](../decisions/toolchain-versions.md), [macos-target](../decisions/macos-target.md)

## Environment

| Parameter | Value |
|---|---|
| Mac | Apple M4, `arm64`, 16 GiB, GPU Apple M4 (Metal 4) |
| macOS | 27.0.1 (26A434) |
| Xcode | Command Line Tools 2418 (full Xcode is not installed and not needed) |
| Go | 1.27.2 (`brew install go`, 2026-10-14) |
| Wails | v2.16.0 via `go tool wails` |
| Node.js / npm | 26.11.1 / 11.20.0 (Homebrew; updated from 26.0.0 / 11.12.1 on 2026-10-15; the same `brew upgrade` also updated the dependent `bitwarden-cli`) |
| Working directory | the repository root; all commands below are relative to it |

`go tool wails doctor` → `SUCCESS Your system is ready for Wails development!` (the optional Xcode, upx, nsis are `Available`, not installed).

## Session architecture plan

- **Actual state before the task:** documentation only; no code, `go.mod` or `frontend/`.
- **Boundaries:** the facade `internal/app` (the single bind), the own bridge `frontend/src/bridge`, the UI `frontend/src/features/runtime-check`, the entry point `main.go`, following [architecture §2–§3](../../APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first). The architecture does not change; the actual paths match the map.
- **Reuse:** the Wails `react-ts` template was generated in a temporary directory only as a reference for `Info.plist`, `wails.json` and the icon; its code (`Greet`, CSS, fonts) was not carried over.
- **Testability:** `main()` without branches; the logic is in `run(start, stderr)` with an explicit dependency; the React component receives the loading function as a prop.
- **Risks checked first (step 0, outside the repository):** `go tool wails` without a global CLI: `pass`; writing coverage counters when the `.app` exits via an Apple Event: `pass` (the `covcounters.*` counters appear, `main.go` has counter 1); TypeScript 7.0.2 + Vite 8 + Vitest 5: `pass`; a click in WKWebView via System Events: `pass` after Accessibility permission for the terminal (granted by the owner).

## RED: tests before the implementation

| Test | Command | Result (expected `fail`) |
|---|---|---|
| `internal/app/app_test.go` | `go test ./internal/...` | `undefined: New`, `undefined: RuntimeInfo`: build failed |
| `main_test.go` | `go test .` | `undefined: run`: build failed |
| 4 frontend test files | `npx vitest run` (in `frontend/`) | `Failed to resolve import "./runtimeInfo" / "./RuntimeCheck" / "./App" / "./main"`; Test Files 4 failed |

## GREEN: actual checks

| # | Check | Command | Result |
|---|---|---|---|
| 1 | Go tests | `go test ./...` | `pass` (2 packages) |
| 2 | TS tests and coverage | `npm --prefix frontend run test:coverage` | `pass`: 4 files, 10 tests; Statements 19/19, Branches 10/10, Functions 6/6, Lines 19/19; "4 files fully covered" |
| 3 | The threshold catches a non-imported file | a temporary `src/zzUncovered.ts` without tests → `vitest run --coverage` | `pass` (expected threshold `fail`): 4 × `ERROR: Coverage … does not meet global threshold (100%) for src/zzUncovered.ts`; the file was deleted |
| 4 | Go 100% with raw counters | unit `-test.gocoverdir` + instrumented `.app` (`GOFLAGS="-cover -coverpkg=./..." go tool wails build`), click, exit; `go tool covdata textfmt`; `awk 'NR>1 && $NF==0'` | `pass`: 6 blocks (`main.go` ×4, `internal/app/app.go` ×2), all with counter 1; zero-count blocks: 0; `covdata percent`: `octopustorrent` 100.0%, `octopustorrent/internal/app` 100.0%. Unit tests only: `main` 83.3%, only `main()` is uncovered |
| 5 | Formatting and analyzers | `gofmt -l .`; `go vet ./...`; `npm --prefix frontend run typecheck` | `pass`: empty; clean; `tsc --noEmit` without errors |
| 6 | Bindings | `go tool wails generate module` | `pass`: `frontend/wailsjs/go/app/App.{js,d.ts}`, `models.ts`, `runtime/` |
| 7 | Release build | `go tool wails build -clean -trimpath` | `pass`, exit 0, ~3 s: `build/bin/OctopusTorrent.app` |
| 8 | Package contents | `find`, `file`, `lipo -archs`, `du -sh` | `Contents/{Info.plist, MacOS/OctopusTorrent, Resources/iconfile.icns, _CodeSignature/CodeResources}`; Mach-O 64-bit `arm64`; 8.4 MB |
| 9 | Dynamic dependencies | `otool -L` | `pass`: only `/usr/lib/{libresolv.9, libobjc.A, libSystem.B}` and `/System/Library/Frameworks/{Cocoa, AppKit, Foundation, CoreFoundation, CoreGraphics, WebKit, Security, UniformTypeIdentifiers (weak), UserNotifications (weak)}`; no `/opt/homebrew` |
| 10 | Signature | `codesign -dv --verbose=2`; `codesign --verify --deep --strict`; `spctl -a -vv` | `Identifier=dev.tsykvas.octopustorrent`, `Signature=adhoc`, `TeamIdentifier=not set`; verify `pass`; `spctl`: `rejected` (expected without Developer ID/notarization) |
| 11 | Info.plist | `plutil -p …/Info.plist` | `CFBundleIdentifier dev.tsykvas.octopustorrent`, `LSMinimumSystemVersion 27.0`, `CFBundleShortVersionString 0.0.1` |
| 12 | No network/external UI resources | `grep '<script\|<link' frontend/dist/index.html`; URLs in `dist/assets/*.js` | `pass`: one `<script src="/assets/index-….js">` (local); the URLs in the bundle are an XML namespace (`w3.org`) and the message text `react.dev/errors/`, not loadable resources |
| 13 | Absolute paths in the binary | `strings … \| grep -c /Users/` | without `-trimpath`: 142 → with `-trimpath`: **0**; `go version -m`: `-trimpath=true` |
| 14 | Development mode | `go tool wails dev -noreload` in the background; click; stop with `SIGINT` | `pass`: Vite on `localhost:5174` (5173 is taken by another project of the owner), the Go response in the window; after stopping, there are no `wails dev`/`vite` processes of this project |
| 15 | Launching the `.app` (LaunchServices) + click, twice | `open build/bin/OctopusTorrent.app`; `osascript click.applescript OctopusTorrent "Check Go connection"`; `osascript … to quit` | `pass` ×2: window text `Go go1.27.2 · darwin/arm64`; no child processes; no `go`/`node`/`wails` processes of this project; clean exit |
| 16 | **Opening from Finder** | owner: double-click in Finder, pressing the button; me: reading the window text via Accessibility | `pass`: the owner sees `Go go1.27.2 · darwin/arm64`; process `ppid=1 (/sbin/launchd)`; the window text read matches; quit ok |
| 17 | **Without internet**, repeated launch | `offline_check.sh` (the owner turns off the network; the script waits for the route to disappear, launches the `.app` twice, presses the button, counts sockets, closes it) | `not_run`: scheduled with the owner |
| 18 | Build from a clean copy | export of `git ls-files --cached --others --exclude-standard` into a temporary directory → `go tool wails build -clean -trimpath` (it runs `npm ci` with `.npmrc` itself) | `pass`: "Installing frontend dependencies: Done", built; `frontend/dist/assets/*.js` has the same SHA-256 (`9a430be283b183dd…`); the unsigned binary has the same SHA-256 `2cd78877…4fc5e679` (reproducible build) |
| 20 | The age of every locked version is ≥ 14 days | `check_release_age.py`: publication time from registry.npmjs.org for all `package-lock.json` entries and from proxy.golang.org for `go list -m all` | `pass`: npm 159, Go 142 versions; none younger than the 2026-09-30 limit; the newest are `chai@6.3.0` (2026-09-30), `wails/v2@v2.16.0` (2026-09-14) |
| 21 | Vulnerabilities | `npm --prefix frontend audit`; `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | `pass`: `found 0 vulnerabilities`; `No vulnerabilities found` |
| 22 | npm install scripts | `npm install-scripts ls` | one package (`fsevents@2.3.3`), forbidden via `allowScripts`; `npm ci` without warnings; `require('fsevents')` loads (prebuilt binary) |
| 19 | Documentation links | `check_links.py` for the Git Markdown files | see "Summary" |

The session scripts (`click.applescript`, `offline_check.sh`, `check_release_age.py`, the link check) stayed in a temporary directory as one-off manual verification tools. A permanent automated check with tests is created by [004](../../tasks/004-reproducible-check-commands.md).

## Version review 2026-10-15

The owner asked to keep the stack up to date. I first updated the frontend to the newest patch versions (react 19.3.1, vite 8.3.4 and others, some released 1–8 days before) and Node to 26.11.1. The owner halted this departure from the 14-day cooldown, treating it as a supply chain vulnerability. Outcome:

- the frontend was returned to versions no younger than 14 days; the rule was extended to transitive packages via `frontend/.npmrc` `min-release-age=14` and the lockfile was regenerated;
- it was found that Go 1.27.2 was 12 days old at the first choice, so the original rule had been violated; the owner approved a separate rule for tools: Go/Node patch releases immediately, new lines after 14 days (Go 1.27.2 and Node 26.11.1 stay);
- transitive Go modules follow Go MVS (as in Wails), vulnerable ones are forced; `govulncheck` and `npm audit` are clean;
- after the changes, items 1–7, 9–13, 15, 18 were repeated: `pass`; Go coverage is 100% again (0 of 6 blocks with a zero counter).

Policy: [toolchain-versions](../decisions/toolchain-versions.md); automation of the age/vulnerability/update checks has been added to the scope of [004](../../tasks/004-reproducible-check-commands.md).

## REFACTOR and debt check

- The Wails template code was not carried over; `main` was reduced to a single call so that the instrumented run covers it fully.
- Found during verification and fixed: the developer's absolute paths in the binary (`-trimpath` in the release command); the Wails cache `frontend/package.json.md5` was added to `.gitignore`.
- After the changes, the tests, coverage and build were repeated: green.
- No TODOs; no temporary workarounds. The prototype UI is an intermediate result until 034/035/040, not debt.

## Compatibility check limitations

- Checked **only** on macOS 27.0.1 on Apple M4 (`arm64`). Other macOS versions and Intel Macs were not checked and are not claimed (`LSMinimumSystemVersion 27.0`).
- Ad-hoc signature; Gatekeeper rejects the package in the `spctl` check. It opens because a locally built package has no quarantine. Moving it to another Mac via download was not checked (066).
- The linker warning `built for newer 'macOS' version (13.0) than being linked (11.0)` does not affect launching (see ADR toolchain-versions).
- ESLint/React analyzers and a single "check everything" command: task 004.

## Summary

`not_run` for item 17 (offline): task 003 stays `in_progress` until it is done.
