# 012 — Check the macOS Trash and file path protection

**Status:** `done`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [003](003-wails-macos-build-spike.md), [007](007-engine-api-lifecycle-spike.md), [069](069-native-code-coverage-gate.md)  
**Requirements:** `DEL-02`, `DEL-04`, `STACK-01`

## Outcome

Verify a supported way of moving files to the macOS Trash and the protection of access to physical paths.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `DEL-02`, `DEL-04`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Choose a system way of moving items to the Trash that returns a separate result for each file.
2. On the pinned Go version, check access relative to the allowed root, symbolic links and concurrent renaming of paths.
3. In our own test directory, check unrelated neighboring files, shared paths and substitution of a file between the check and the operation.
4. Record the confirmed protection boundaries and the cases the chosen implementation does not support.
5. Verify that a Trash error does not trigger irreversible deletion; describe the separate results of the file operation and of the SQLite transaction.

## Artifacts and change locations

**Proposed locations:** `internal/platform prototype; docs/verification/trash.md`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/012-macos-trash-and-path-spike.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] Only explicitly selected test files end up in the Trash.
- [x] An error contains the exact list of files that remained on disk.
- [x] All checks are performed on specially created test data and do not touch personal files.
- [x] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Check symbolic links, substitution of the target file, missing write permission and Unicode names; the operation must stay within the allowed area and return an explicit result.
- Pass an overly broad root directory and cause a Trash error; the implementation must refuse the unsafe operation without irreversible deletion.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Do not replace an unverified system Trash API with the `rm` command or any other means of irreversible deletion. SQLite and the file system do not form a shared atomic transaction, so a partial result must be accounted for explicitly.

**Basis for technical decisions:** [Go filesystem roots](https://go.dev/blog/osroot). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

## Execution result

**Date:** 2026-10-19. **Status:** `done` (with [069](069-native-code-coverage-gate.md), which added the native coverage the policy required).

- **Changes:** new `internal/platform` (macOS, the repo's first cgo). `Trash(saveDir, names, Trasher)`
  moves each named file to the macOS Trash with `NSFileManager trashItemAtURL` (`trash_darwin.m`,
  `-framework Foundation`) behind a `Trasher` interface. Confinement is structural (`os.OpenRoot`);
  a name must be `filepath.IsLocal` and the move acts on the opened file's own path (`fcntl
  F_GETPATH`), so it cannot be redirected by its name; a non-regular file (symlink, FIFO, device) is
  refused by `Lstat` before any open; a volume root (Statfs `f_mntonname`) or the home is refused; a
  bad path to the native `Trasher` is refused in Go so NSString/NSURL cannot abort the process. One
  `Result` per name and `ErrSomeRemain` naming the files that stayed (DEL-04); never `os.Remove`.
  ADR [trash-and-paths](../../docs/decisions/trash-and-paths.md), feature
  [doc](../../docs/app/trash.md), report [012](../../docs/verification/012-macos-trash-and-path-spike.md).
- **Checks:** `go tool devtool check` → `GATE PASSED`; `trash_darwin.m` is measured at 100% by the
  new `native coverage` step (069); `internal/platform` is 100% Go (fake `Trasher` on temp dirs +
  the real `NSFileManager` on an APFS disk image, so nothing reaches `~/.Trash`).
- **Review:** the three-agent review found and this change fixed a reproduced confinement escape and
  a volume-root miss; the last gap, a coverage measurement for the `.m` (TDD_AND_QUALITY §3), is
  closed by [069](069-native-code-coverage-gate.md), delivered together.
- **Limitations:** the full UI→Go→Trash flow is `not_run` (no delete UI yet; owner decision); a
  regular→regular same-name swap, or an intermediate-directory swap, inside the folder between the
  check and the move is a path-based-API residual for 032 (the move is reversible).
- **Next:** [020](020-destination-filesystem-service.md) (folder selection, collisions), [032](032-safe-payload-trash-journal.md) (partial-result journal); the facade wires `Trash` in a delete task.
