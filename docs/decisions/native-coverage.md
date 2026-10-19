# Measuring native (cgo C/Objective-C) code coverage in the gate

**Status:** `accepted`  
**Date:** 2026-10-19  
**Task:** [069](../../product/tasks/069-native-code-coverage-gate.md)  
**Register item:** new  
**Requirements:** `STACK-01`, `STACK-05`, `QA-01`, `QA-02`, `QA-05`

## Question

Task 012 introduced the repo's first native code, `internal/platform/trash_darwin.m`. The policy
([TDD_AND_QUALITY §3](../engineering/TDD_AND_QUALITY.md)) requires a coverage measurement for own code
in another language before it is accepted, and the gate measured only Go and TypeScript. We need a
measurement that is as strict as the Go/TS ones (100%, inventory from the listing, no silent
narrowing) and runs in `go tool devtool check`.

## Decision

A `native coverage` gate step (`internal/devtool/nativecov.go`) using clang source-based coverage.

- **Inventory from the listing:** every own `.m`/`.c` under `internal/` or `cmd/`, grouped by package
  — the same principle as the Go and TS inventories, so it cannot be narrowed by hand. No native
  code → the step passes trivially.
- **Instrumented build, scoped by the environment:** `env CGO_CFLAGS="-fprofile-instr-generate
  -fcoverage-mapping" CGO_LDFLAGS="-fprofile-instr-generate" go test -c` builds a coverage-only test
  binary; the product and release builds are never instrumented.
- **Explicit profile flush:** a Go test binary exits through `os.Exit`, which skips the C runtime's
  atexit profile writer, so the raw profile is empty. The package's `TestMain` calls
  `internal/testutil/nativecov.Flush` (test support, never linked by the product) before `os.Exit`. That shim calls
  `__llvm_profile_write_file`, which we define weakly (a no-op) so the normal build links; the profile
  runtime's strong definition overrides it in the instrumented build. The shim's C lives in a `.go`
  cgo preamble, not a `.m`/`.c` file, so it is not itself a measured native source (no
  self-referential paradox); its one Go line is covered by the package's normal unit run.
- **Measure and require 100%:** `xcrun llvm-profdata merge` then `xcrun llvm-cov export -summary-only`;
  every region of every own native source must be covered, or the step fails naming the source, like
  the Go/TS coverage steps.
- **Unavailable tools:** missing `xcrun`/`llvm-cov` is `not_run` (an unavailable environment), never a
  silent pass.
- **Unit-tested behind the `System` seam:** the step runs its external tools through `devtool.System`,
  so it is tested at 100% with the existing fake, scripting the build, run, merge and a canned
  `llvm-cov export` JSON. The real end-to-end measurement is exercised by the gate itself.

## Alternatives considered

| Option | Why not |
|---|---|
| Leave the `.m` unmeasured (ADR exception) | Violates the policy; the owner chose to build the tool |
| `gcov`/`lcov` | clang source-based coverage is the native match for the toolchain already present (Xcode `llvm-cov`); no extra dependency |
| dlsym the profile writer at runtime | The statically-linked symbol is not in the dynamic table, so dlsym returns null; the weak definition overridden by the runtime is what links and flushes |
| A build tag for the flush shim | A tag-gated `.go` file is absent from the default build and would show as uncovered in the Go inventory; the weak-symbol shim is always compiled and its Go line is covered |

## Consequences

- `internal/platform/trash_darwin.m` is measured at 100%, which unblocks [012](../../product/tasks/012-macos-trash-and-path-spike.md); the two complete together.
- A macOS adapter added inside `internal/platform` is measured with no new work. A native source in a
  **new** package needs that package's `TestMain` to call `internal/testutil/nativecov.Flush` before
  `os.Exit`, or its profile is empty and the step fails loudly (never a silent pass).
- The step adds an instrumented build and run of each package that has native code (only `platform`
  today) to every gate run.
- It relies on `xcrun llvm-profdata`/`llvm-cov` and a small, stable subset of the export JSON; a
  future Xcode change to that JSON would be caught by the gate run and the parse is tiny.

## Verified grounds

[Report 069](../verification/069-native-code-coverage-gate.md): Apple M4, macOS 27, Go 1.27.2, Xcode
`llvm-cov`. The step's logic is at 100% in `internal/devtool` with a fake `System`; the real run in
`go tool devtool check` reports `internal/platform/trash_darwin.m` with every region covered, and the
normal and instrumented builds both link (the weak-symbol flush).
