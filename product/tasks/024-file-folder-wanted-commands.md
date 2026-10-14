# 024 — Manage selection of files and all descendants of a folder

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [023](023-durable-import-commit.md), [013](013-transfer-policy-state-machine.md)  
**Requirements:** `TR-06`, `TR-07`, `UI-09`, `UI-18`

## Outcome

Persist the file selection and apply it to a single file or to all descendants of a folder independently of the torrent's manual pause.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-06`, `TR-07`, `UI-09`, `UI-18`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): the API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Persist `wanted` and the file's previous priority; apply `PiecePriorityNone` or restore the previous priority.
2. For a folder command, determine all descendants according to the verified model, including collapsed folders and already completed files.
3. Write the desired selection in a single transaction and apply it to the engine sequentially.
4. Explicitly report the absence of selected files and a partial application error; keep `manualPaused` unchanged.
5. Do not create hidden readers for excluded files; verify the behavior of torrent pieces shared across the boundaries of adjacent files.

## Artifacts and change locations

**Proposed locations:** `internal/app/file_policy.go`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/024-file-folder-wanted-commands.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] For a partially selected folder, the start command selects all descendants; for a fully selected one, the pause command excludes all of them.
- [ ] Excluding a file does not delete already received bytes and does not reset its progress to zero.
- [ ] The card and the details panel receive a consistent snapshot with a single revision.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Exclude a file with a shared boundary piece and, separately, an already completed file; the other selected files must continue downloading, and the existing bytes must remain.
- Change the selection of collapsed descendants, exclude all files and crash the process after the DB write but before the engine change; the restore must apply the persisted selection without losing the manual pause.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The file pause button means stopping its further targeted download. A shared torrent piece may require writing boundary bytes of an excluded file for the adjacent selected one. Excluding a completed file does not guarantee that its upload stops.

**Basis for technical decisions:** [File source](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go). During implementation, check the signatures and constraints against the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
