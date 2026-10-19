# Report 069: native (cgo/Objective-C) code coverage in the gate

**Task:** [069](../../product/tasks/069-native-code-coverage-gate.md). **Requirements:** `STACK-01`, `STACK-05`, `QA-01`, `QA-02`, `QA-05`. **ADR:** [native-coverage](../decisions/native-coverage.md). **Branch:** `task/012-macos-trash` (delivered with 012).

## Session architecture plan

Approved by the owner on 2026-10-19 (after 012's review found that the policy requires a native
coverage measurement the gate did not have). See the ADR for the decision.

## What was built

- `internal/devtool/nativecov.go`: the `native coverage` gate step — discovers own `.m`/`.c` from the
  repository listing, builds an instrumented test binary per package (`CGO_CFLAGS`/`CGO_LDFLAGS`
  scoped to that build), runs it, merges and exports with `xcrun llvm-profdata`/`llvm-cov`, and
  requires every region covered. Missing tools → `not_run`.
- `internal/testutil/nativecov` (`Flush`): a weak-symbol `__llvm_profile_write_file` shim, test
  support (not linked by the product), called by a native package's `TestMain` before `os.Exit`, so
  the instrumented profile is written.
- Wired into `check.go` after `go coverage`, in every non-`deps` mode.

## RED → GREEN

| Step | Command | Result |
|---|---|---|
| step unit tests | `go vet ./internal/devtool` with `nativecov_test.go` only | `fail` as expected: `undefined: NativeCoverage` |
| | `go test ./internal/devtool` | `pass`, 100%: pass, an uncovered region, an unreported source, each tool failure (build/run/merge/export/bad JSON/empty data), `not_run` without the tools, a mkdir failure, no-native-code |
| flush shim links both ways | `go test ./internal/platform` (normal) and an instrumented `go test -c` | both link; the normal build no-ops the flush, the instrumented build writes a non-empty profile (dlsym and `weak_import` were tried first and failed — a weak definition overridden by the profile runtime is what works) |
| real measurement | `go tool devtool check fast` | `pass native coverage: every region of every own native source is covered: internal/platform/trash_darwin.m` (9/9 regions) |
| full gate | `go tool devtool check` | `GATE PASSED` |

Working directory `/Users/tsykvas/projects/torrent`; data: synthetic, a scripted fake `System` for
the unit tests and the real `internal/platform` package for the in-gate run; `-race` for the Go
tests; exit 0.

## Coverage

`go test -count=1 -cover ./internal/devtool` → `ok ... coverage: 100.0% of statements` (the step is behind the `System` seam, tested with the fake; `go test ./internal/testutil/nativecov` 100% too).
`internal/platform` stays 100% Go; `trash_darwin.m` is now measured by the new step at 100% (9/9
regions). The flush shim's C is a `.go` preamble, not a measured native source; its Go line is
covered by the platform unit run.

## Refactoring and debt

- The step reuses the gate's `System` seam and the `env VAR=val cmd` form from `appcov`; no new
  dependency (a tiny JSON subset is unmarshalled).
- `relToRoot` was dropped in favour of matching export files by the absolute paths passed, so no
  un-coverable fallback branch remained.
- Completes [012](../../product/tasks/012-macos-trash-and-path-spike.md): it moves to `done` in this
  change.
- No `TODO`, skipped test, ignore directive or lowered threshold. Open for later: more native
  adapters (measured automatically); a future Xcode JSON change would surface in the gate run.
