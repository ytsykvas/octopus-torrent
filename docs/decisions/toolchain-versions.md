# Tool and dependency versions, update policy

**Status:** `accepted`  
**Date:** 2026-10-14, policy refined on 2026-10-15 by the owner's decision  
**Task:** [003](../../product/tasks/003-wails-macos-build-spike.md)  
**Register item:** O2 from the [baseline description](project-baseline.md#6-open-decision-register) — partially: Go, Wails, Node.js, frontend. The anacrolix/torrent version is fixed by [007](../../product/tasks/007-engine-api-lifecycle-spike.md), the SQLite driver by [008](../../product/tasks/008-sqlite-driver-durability-spike.md); both follow the policy below.  
**Requirements:** AI-07, REL-01, STACK-05

## Question

1. Which set of Go, Wails v2, Node.js and frontend dependency versions to take now so that the `.app` builds reproducibly from a clean copy.
2. How to keep the stack current without exposing the project to freshly published broken or malicious releases (supply-chain attacks).

## Decision

### Version policy (mandatory for all subsequent tasks)

| What | Rule |
|---|---|
| **Third-party packages** — npm and Go modules, **including transitive ones** | The latest stable version published **at least 14 days ago**. Pre-releases (`alpha`, `beta`, `rc`) are not used. |
| **Tools** — Go, Node.js from official channels (go.dev / Homebrew) | Patch releases of an already adopted line are installed immediately: they often fix vulnerabilities, and the channels are signed and have checksums. A new minor or major line only after 14 days. |
| Transitive Go dependencies | The versions Go MVS selects from the requirements of direct dependencies (as the library author releases them). Only a transitive module with a known vulnerability is force-upgraded. |
| npm install scripts | Forbidden by default: an explicit list in `allowScripts` (`frontend/package.json`). Currently `fsevents: false` — the package ships with a prebuilt binary, the script is not needed. |
| Currency | During every task that changes dependencies, and by the regular check from [004](../../product/tasks/004-reproducible-check-commands.md) (`npm outdated`, `go list -m -u all`, release age, `npm audit`, `govulncheck`): a version that has left the 14-day quarantine is updated, not postponed. |
| Wails v3 | Remains `beta` (`v3.0.0-beta.29` of 2026-10-13). Migration is a separate owner decision after a stable release ([requirements §1](../../product/PROJECT_REQUIREMENTS.md#chosen-stack)). |

**Technical enforcement:** `frontend/.npmrc` → `min-release-age=14`: npm itself will not install a younger version of any package, including transitive ones (npm ≥ 11.20). Go has no equivalent option — module age is checked by publication time in `proxy.golang.org` (see "Verified grounds"); 004 automates this.

### Current set

| Component | Version | Release date | Where pinned |
|---|---|---|---|
| Go | 1.27.2 | 2026-10-02 (patch of the 1.27 line from 2026-08-18) | `go.mod`: `go 1.27.2` (an older local Go will download it via `GOTOOLCHAIN=auto`) |
| Wails v2 — library and CLI | v2.16.0 | 2026-09-14 | `go.mod` / `go.sum`; CLI as `tool github.com/wailsapp/wails/v2/cmd/wails`, invoked as `go tool wails …` |
| Node.js / npm | 26.11.1 / 11.20.0 | 2026-10-07 (patch of line 26) | the version verified here; `frontend/package.json` → `engines.node >=22.12.0` (Vite 8 requirement) |
| react, react-dom | 19.3.0 | 2026-09-09 | `frontend/package.json` + `package-lock.json` |
| typescript | 7.0.2 | 2026-07-08 | same |
| vite / @vitejs/plugin-react | 8.3.1 / 6.1.1 | 2026-09-24 / 2026-08-28 | same |
| vitest / @vitest/coverage-v8 | 5.0.2 / 5.0.2 | 2026-09-25 | same |
| jsdom | 30.1.1 | 2026-09-22 | same |
| @testing-library/react, user-event, jest-dom | 16.3.3, 14.6.7, 7.0.1 | 2026-08-27, 2026-09-02, 2026-08-09 | same |
| @types/react, @types/react-dom | 19.3.0 | 2026-09-09 | same |

Newer versions still in quarantine as of 2026-10-15: react 19.3.1, vite 8.3.4, @vitejs/plugin-react 6.1.2, vitest and @vitest/coverage-v8 5.0.4, jsdom 30.1.2, user-event 14.6.8. Update them once they are 14 days old.

The release build is `go tool wails build -clean -trimpath`; `-trimpath` removes the developer's absolute paths from the binary (without it there are 142 of them).

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Always the newest versions without a pause | Maximum freshness | A freshly published broken or malicious package enters the project before it is detected | Rejected by the owner on 2026-10-15 as a vulnerability |
| A 14-day pause for Go/Node patch releases too | One rule for everything | Delays security fixes for tools; requires manually installing older versions outside Homebrew | Owner's decision: tool patch releases immediately |
| `go get -u all` (all transitive Go modules to the newest) | The freshest transitive versions | A combination the Wails authors have not tested (v0→v1 jumps, e.g. lipgloss, glamour); risk of breaking the CLI/dev mode unnoticed by tests | Owner's decision: transitive per MVS, vulnerable ones forced |
| Global `go install …/wails@v2.16.0` | Standard Wails instructions | The CLI version is not tied to the repository; `~/go/bin` must be in `PATH` | `go tool` provides the same CLI, pinned in `go.mod` |
| TypeScript 6.0.3 | Widest compatibility | A line going obsolete | 7.0.2 verified with Vite 8 and Vitest 5 |

## Verified grounds

Environment: Apple M4, macOS 27.0.1, Xcode Command Line Tools 2418 ([report 003](../verification/003-wails-macos-build-spike.md)).

- **Release age (2026-10-15, cutoff 2026-09-30):** the publication time of every version from `frontend/package-lock.json` (159, registry.npmjs.org) and from `go list -m all` (142, proxy.golang.org) was checked — `pass`, none are younger; the newest are `chai@6.3.0` (2026-09-30) and `wails/v2@v2.16.0` (2026-09-14).
- **Vulnerabilities:** `npm audit` — `found 0 vulnerabilities`; `govulncheck ./...` — `No vulnerabilities found`.
- `go tool wails doctor` — `SUCCESS`; `go tool wails version / init / generate module / build / dev` work without a global `wails`.
- Tests, 100% coverage, `tsc --noEmit`, build and a click in the real `.app` — `pass` on this set.
- Build from a clean copy of tracked files (`npm ci` from the lockfile under `min-release-age=14`) — `pass`; the unsigned binary and the frontend bundle are byte-for-byte identical to the build from the working tree.

**Corrected error:** the first revision of this ADR (2026-10-14) declared the 14-day rule but took Go 1.27.2 at 12 days old and did not check transitive npm packages. Now age is checked for all packages, and a separate rule above applies to tools.

Not verified: other Node.js versions within `>=22.12.0`.

## Consequences

- The commands in the [README](../../README.md#build-and-check-commands) use `go tool wails`; a global Wails CLI is not needed.
- Adding or updating a dependency goes together with updating the "Current set" table and rechecking age, vulnerabilities, tests and the `.app`.
- [004](../../product/tasks/004-reproducible-check-commands.md) includes in the single local check: release age, `npm outdated` / `go list -m -u all` (report of available updates), `npm audit`, `govulncheck`.
- Known linker warning `object file … built for newer 'macOS' version (13.0) than being linked (11.0)` — Wails links with minimum version 11.0, Go 1.27 builds objects for 13.0; it does not affect launch. The target system is in [macos-target.md](macos-target.md).
