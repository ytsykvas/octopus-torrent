# 027 — Publish accurate progress and state snapshots

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [024](024-file-folder-wanted-commands.md), [025](025-pause-resume-application.md), [026](026-rate-limits-seed-network.md), [005](005-domain-dto-and-bridge-contract.md)  
**Requirements:** `TR-02`, `UI-01`, `UI-02`, `UI-08`, `UI-12`, `UI-19`, `STACK-02`

## Outcome

Publish a single consistent state snapshot for the card, the details and all other screens.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-02`, `UI-01`, `UI-02`, `UI-08`, `UI-12`, `UI-19`, `STACK-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): the API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Compute speeds from the delta of payload counters and monotonic time; BytesCompleted is not a network speed counter.
2. Compute the progress of selected real files and folders; exclude service pad files and division by zero.
3. Apply the status priority table from 013; hide speeds for pause/completion and show the time to completion only when it is computed correctly.
4. Provide a full snapshot and events with a revision; aggregating frequent updates does not lose errors and operation completions.
5. Measure the publication frequency, bound the queue and end subscriptions after a torrent is removed or the application is closed.

6. Publish `runtimeID` and revisions according to 005. Explicitly distinguish a full snapshot from a partial event: coalescing full updates is not a loss of a mandatory delta. Important operation completions remain available after a missed event.

## Artifacts and change locations

**Proposed locations:** `internal/app/snapshot_publisher; status projection`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** a `docs/verification/027-metrics-status-snapshots.md` record or an exact reference to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The card and the details show the same revision.
- [ ] Corrupted or unselected data does not produce a false completion.
- [ ] Recovery after a missed event distinguishes full snapshots from partial updates.
- [ ] The required checks have been performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Verify a counter reset, incorrect time and a decrease in progress after a hash check.
- Verify missed/reordered events, a UI reload and many files.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, of the test UI and of system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Full snapshots may be aggregated, skipping intermediate revisions. A missed partial update requires restoring the full state. Distinguish these cases so as not to create endless repeated requests.

**Basis for technical decisions:** [Wails events](https://wails.io/docs/reference/runtime/events/), [Stats](https://pkg.go.dev/github.com/anacrolix/torrent). Check the signatures and constraints against the pinned version; reading the API is not a passed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): after a frontend reload React has lost its operation IDs: add pending operations to the snapshot or a `ListOperations` method (with 035).

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
