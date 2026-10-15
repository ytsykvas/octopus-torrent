# Local quality gate: one command, independent coverage checks, analyzers

**Status:** `accepted`  
**Date:** 2026-10-15  
**Task:** [004](../../product/tasks/004-reproducible-check-commands.md)  
**Register item:** O3 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `AI-07`, `AI-08`, `QA-01`, `QA-02`, `QA-05`

## Question

QA-05 requires a local command that blocks acceptance on failed tests, coverage below exactly 100%,
an incomplete source inventory, formatting, type or analyzer errors, stale bindings and outdated
documentation. Until 004, the gate was a list of seven manual steps in `/check`. Go's 100% depended
on a manual click in the `.app`, TS coverage on Vitest's own thresholds (which a narrowed `include`
silently bypasses), there was no linter, and dependency age was checked by a one-off script.

## Decision

- **One command**, `go tool devtool check`, in `internal/devtool` (Go, so the 100% rule applies to the
  gate itself). It runs every step in order and never stops early; each step prints one line,
  `pass`, `FAIL` or `not_run`, and the run ends with `GATE PASSED`, `GATE FAILED` or
  `GATE INCOMPLETE`. Exit 0 only on `GATE PASSED`, or on `GATE INCOMPLETE` in a partial mode.
- **Modes.** `check` is the full gate and the only one that accepts a change. `check offline` skips the
  network steps, `check fast` also skips the `.app` run; skipped steps are `not_run`, so these modes can
  only report an incomplete gate. `check deps` runs the network steps alone. A quick check of one
  changed module uses the tools directly (`go test -race ./internal/<pkg>`,
  `npm --prefix frontend test -- <file>`, `npm --prefix frontend run lint -- <file>`) and is never
  acceptance.
- **Not-run semantics.** A step whose environment is missing (no network, Automation permission
  refused, another OctopusTorrent running) is a mandatory `not_run` and fails the full gate. A tier that
  does not exist yet — real-engine integration (007, 018), test-bridge UI tests (035) — is printed as
  `not_run (not implemented yet)` and does not block; the line disappears when the tier is built.
- **TS coverage, independent of the tool.** Vitest 5.0.3 with `@vitest/coverage-v8` 5.0.3 (pinned,
  four metrics, `perFile` thresholds 100) writes `coverage-summary.json`; the gate then derives the
  inventory from the repository listing (every `frontend/src/**/*.ts(x)` except tests, the test setup
  and `.d.ts`) and requires every file in the report with `covered == total` on lines, statements,
  functions and branches by raw counts, nothing skipped. A narrowed `include`, an excluded file,
  rounding or a missing report fail the gate.
- **Go coverage, raw counters.** `go test -race -covermode=atomic -coverpkg=./...` writes counters for
  the unit run; the gate then builds the `.app` with the same instrumentation, launches it with
  `GOCOVERDIR`, and quits it with an Apple Event (`tell application id … to quit`) — a normal exit, so
  Go writes the counters, which covers `func main`. Both profiles must be non-empty, in one mode and
  built from the same sources (identical blocks for each shared file); they are merged by block, every
  own non-test file with a function must be measured, and no block may have a zero count. Without the
  `.app` run, the zero blocks of `func main` are reported as `not_run`, never as covered.
- **Escape hatches fail the gate:** in `frontend/src`, `v8|c8|istanbul ignore`,
  `eslint-disable`/`oxlint-disable`, `@ts-ignore|expect-error|nocheck`, `it|test|describe` (with any
  modifier such as `.concurrent`) `.skip|only|todo|skipIf|runIf|fails`, and `xit|xtest|xdescribe`; in
  Go, `//nolint` anywhere and `t.Skip` in tests.
- **Analyzers.** Go: `gofmt -l .` must be empty, `go vet ./...`. TS: `tsc --noEmit` (strict, plus
  `noImplicitReturns`) and Oxlint 1.86.0 with oxlint-tsgolint 7.0.2003 (`oxlint --type-aware
  --deny-warnings`, `frontend/.oxlintrc.json`): categories correctness and suspicious are errors, perf
  is a warning that fails through `--deny-warnings`, React hooks rules are errors; the `jsx-a11y`
  plugin is on, and `typescript/ban-ts-comment`, `no-explicit-any` and `no-non-null-assertion` are
  errors.
  `react-in-jsx-scope` is off (automatic JSX runtime), `typescript/consistent-return` is off in favour
  of the compiler's `noImplicitReturns`.
- **Generated bindings** are regenerated with `go tool wails generate module` before any build and
  compared by content (Wails flips file modes, which do not matter).
- **Dependencies** (network): release age ≥ `minReleaseAgeDays` (the one constant of the policy) of every locked
  npm version and every Go module in the build list by registry time; `npm audit`; `govulncheck`
  pinned as a `go.mod` tool; a report of updates of direct dependencies and tools that have been out
  of quarantine for 14 days; a query the report could not make is `not_run`, never an empty report.
  Each locked npm package must be resolved from `https://registry.npmjs.org/` with the registry's
  `integrity` for that version, so `npm ci` installs exactly what was dated; a Go module replaced by a
  local directory fails (no date). npm's effective cooldown (`npm config get min-release-age before
  min-release-age-exclude`) and Go's module settings (`go env -json`: default `GOPROXY` and `GOSUMDB`;
  `GOFLAGS`, `GOINSECURE`, `GONOPROXY`, `GONOSUMDB`, `GOPRIVATE` unset) are checked offline on every
  run. For Go the date is the tagged commit's committer time, set by the publisher
  ([toolchain-versions](toolchain-versions.md#version-policy-mandatory-for-all-subsequent-tasks)); `go.sum` and `govulncheck` do not depend on it.
- **One code version:** the gate fingerprints every repository file before the first step and after
  the last; a change fails the run and names the files.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| ESLint + typescript-eslint | The common React/TS setup | Every typescript-eslint release, 8.71.2 included, requires `typescript <6.1.0`; the project uses 7.0.2, whose JS API is unstable | Cannot run on the pinned TypeScript |
| Biome 2.5.15 | One tool for lint and format | Own rule set, limited type-aware rules | The owner chose Oxlint: ESLint-compatible rules and type-aware rules built on TypeScript 7 |
| Trust Vitest's thresholds | No extra code | `include`/`exclude` decide what is measured; an excluded file is simply absent | Fails "narrowing the scope is forbidden" (QA-05) |
| Unit profile only, `main()` as an allowed exception | No window during the gate | An exception list is exactly what the policy forbids | The `.app` run covers `main()` for real |
| Quit the `.app` with SIGTERM | No Automation permission | The Go runtime does not write coverage counters on SIGTERM | Counters would be missing |
| Shell script for the gate | Short | Untested, `go run` turns exit codes into 1 | Go in `internal/devtool`, covered like the product |
| Network checks only in a separate command | The gate works offline | Easy to forget; supply chain is the owner's main risk | The owner put them in the full gate, with `check offline` for working without a network |

## Verified grounds

[Report 004](../verification/004-reproducible-check-commands.md), Apple M4, macOS 27.0.1:
RED → GREEN for every check function; on this repository `go tool devtool check` → `GATE PASSED` in
23 s; controls (a TS type error, a Go compile error, an untested TS file, the same file excluded in
`vite.config.ts`, an untested Go function) each exit 1; type-aware Oxlint rules work on TS 7.0.2;
the instrumented `.app` writes counters when quit by an Apple Event (also traced in Wails v2.16.0:
`applicationShouldTerminate` → `Quit()` → `wails.Run` returns → `os.Exit`); a clean copy at another
absolute path passes the gate.

## Consequences

- `/check` runs `go tool devtool check`; `.claude/commands/check.md`, `fix-checks.md`, the README and
  `CLAUDE.md` describe the command instead of a step list.
- The full gate opens an OctopusTorrent window for a few seconds, needs a logged-in GUI session, the
  terminal's Automation permission for OctopusTorrent (a one-time macOS prompt) and the network; it
  refuses while another OctopusTorrent runs, so two workspaces cannot run it at the same time.
- A new owned language or source directory needs an inventory rule in the gate before it is
  accepted. New tiers (engine integration, test-bridge UI tests, real `.app` scenarios) replace their
  `not_run` lines when their tasks build them.
- Changing a threshold, an analyzer rule or an inventory rule is a change to this ADR.
- **The `.app` run must not touch the owner's data.** Today `main()` only opens a window. Once startup
  opens the library, logs and the engine ([architecture §6](../../product/APPLICATION_ARCHITECTURE.md#6-sqlite-and-restore)),
  every `check` would open the owner's real library and resume real torrents, and would collide with
  the single-instance lock. The first task that gives the application a data directory (008, 014, 015
  or 028, whichever comes first) must make the gate launch it with an isolated data directory and save
  folder (`open --env …`) and fail if the override is not honoured.
- **Exit 0 for `GATE INCOMPLETE`** in the partial modes is deliberate: they are the TDD loop and the
  network-free run, and the last line says the gate is incomplete. Acceptance is only `GATE PASSED`
  from the full `check`, which exits 1 on any mandatory `not_run`. A caller that needs to tell the two
  apart reads the last line.
- **`-race` in every mode.** Work order item 3 separates fast checks from race-detector checks; at
  today's size `go test -race ./...` adds seconds, so one test run serves both. Split it when it does
  not.
- **No step time limit yet.** A tool that hangs (a stalled `npm ci`, the first-time Automation prompt)
  hangs the gate visibly; the owner interrupts it. Per-step limits through a context arrive with the
  first step that can hang unattended (engine integration, 007/018).
- **Inventory rules still to decide:** `goInventory` counts every non-test `.go` file, including under
  `testdata/`, which `./...` does not build, so such a file fails as "not in the coverage profile";
  shell scripts have no inventory rule. Task 006 (synthetic torrents, possibly `scripts/`) decides
  both under the rule above.
