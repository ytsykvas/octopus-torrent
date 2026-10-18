# 069 — Measure native (cgo/Objective-C) code coverage in the gate

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [004](004-reproducible-check-commands.md), [012](012-macos-trash-and-path-spike.md)  
**Requirements:** `STACK-01`, `STACK-05`, `QA-01`, `QA-02`, `QA-05`

## Outcome

Add a gate step that measures the coverage of our own native code (cgo C/Objective-C, such as
`internal/platform/trash_darwin.m`) and requires 100%, so the policy "new own code in another
language requires a measurement tool before it is accepted" ([TDD_AND_QUALITY §3](../../docs/engineering/TDD_AND_QUALITY.md))
is enforced, not only asserted. This unblocks [012](012-macos-trash-and-path-spike.md).

## Read before starting

- [Task execution rules](README.md), [engineering rules](../../docs/engineering/TDD_AND_QUALITY.md) (§3, the measurement boundaries).
- [quality-gate ADR](../../docs/decisions/quality-gate.md), [devtool](../../docs/tooling/devtool.md), the gate in `internal/devtool/check.go` and `appcov.go`.
- [trash-and-paths ADR](../../docs/decisions/trash-and-paths.md) and [report 012](../../docs/verification/012-macos-trash-and-path-spike.md): the first native file and why it is blocked.

## Work order

1. Discover our own native sources (`.m`, `.c`, `.h` with a body) from the repository listing, the way the Go and TS inventories are derived — not from a hand-kept list.
2. Build an instrumented test binary for each package that has native code (`CGO_CFLAGS=-fprofile-instr-generate -fcoverage-mapping`, `CGO_LDFLAGS=-fprofile-instr-generate`, `go test -c`), run it writing a raw profile, and flush the profile despite the Go test binary's `os.Exit` (a Go test main exits without running the C runtime's atexit writer): a cgo flush shim calling `__llvm_profile_write_file` through a weak symbol, invoked from the package's `TestMain`.
3. Merge (`xcrun llvm-profdata merge -sparse`) and export (`xcrun llvm-cov export`) the region coverage of each native source, and require covered == total; report any uncovered region like the Go and TS checks do.
4. Wire the step into `go tool devtool check` (and the fast/offline modes as appropriate), reported like the other steps; the flush shim is coverage infrastructure, excluded from the measured native inventory the way test files are from Go.
5. Bring `internal/platform/trash_darwin.m` to 100% and set [012](012-macos-trash-and-path-spike.md) to `done`.

## Artifacts and change locations

**Proposed locations:** `internal/devtool` (the new step and its unit tests with a fake `System`), `internal/platform` (the flush shim), `docs/tooling/devtool.md`, `docs/decisions/quality-gate.md` (or a new ADR), `README.md` commands.
**Evidence of completion:** `docs/verification/069-native-code-coverage-gate.md`, and `internal/platform/trash_darwin.m` shown at 100% by the new step.

## Acceptance criteria

- [ ] The gate measures every own native source and fails if any region is uncovered; `trash_darwin.m` is at 100%.
- [ ] The measurement is derived from the repository listing, not a hand-kept include list, and cannot be silently narrowed.
- [ ] The `devtool` code added for this is itself at 100% by the project's metrics, with a fake `System` for the external tools.
- [ ] All checks below are run at the appropriate level; a check not run or failed is not marked passed.

## Checks

- A package with a fully covered native file passes; one with an uncovered region fails and names it.
- A missing `xcrun`/`llvm-cov`, or an empty profile, is a `fail`/`not_run`, never a silent pass.
- The instrumented build and the normal build both link (the flush shim's weak symbol resolves in both).

**How to run:** the commands recorded in [004](004-reproducible-check-commands.md) and the root README; record the exact command, working directory, configuration, exit code and result.

## Decisions, risks and boundaries

The Go test binary exits with `os.Exit`, so the C profile runtime's atexit writer does not run; the profile must be flushed explicitly. Instrumentation is enabled only for the dedicated coverage build (via environment), never for the product or release build. Keep the external tools (`xcrun`, `llvm-cov`, `llvm-profdata`) behind the `devtool.System` seam so the step is unit-tested without them. The flush shim is coverage infrastructure, not measured product logic.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md). RED → GREEN → REFACTOR; 100% of all own executable code by the exact metrics; update the feature document and the [docs index](../../docs/README.md) in the same change; record significant decisions in an ADR. Completion is blocked if tests, coverage or documentation are missing or debt remains.

## Handoff to the next executor

- Update the status here and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded, and set [012](012-macos-trash-and-path-spike.md) to `done` in the same change.
- Record a short outcome: what changed, exact checks with statuses, ADR, actual change locations and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision and the way to unblock.
