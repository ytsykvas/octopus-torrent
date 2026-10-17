# 035 — Implement the React bridge and snapshot resync

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [005](005-domain-dto-and-bridge-contract.md), [004](004-reproducible-check-commands.md)  
**Requirements:** `STACK-02`, `AI-04`, `AI-08`

## Outcome

Create a single React adapter and a snapshot store with subscription cleanup and state resynchronization.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `STACK-02`, `AI-04`, `AI-08`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Define AppBridge for the real and the test environment; the browser adapter does not start the engine.
2. Subscribe before the initial snapshot, discard stale revisions and recover gaps according to the contract.
3. Separate the UUIDs selected for details and the expanded folders from the Go business state.
4. Track pending operations; a late response does not return the UI to an old state.
5. On unmount/StrictMode, clean up listeners; connect the real adapter for good after 034.

6. Track the active connection and `runtimeID` according to 005; after a launch change, request the authoritative full state. Ignore responses and events of an old connection generation, even if their revisions are higher.
7. Do not trigger state refetching because of skipped full snapshot numbers. For partial events, check the base revision; bound the queue, the parallel resync requests and the retries after an error.
8. After the interface reloads, restore the state of non-trivial operations from Go; the disappearance of a completion notification does not mean the command must be executed again.

## Artifacts and change locations

**Proposed locations:** `frontend/src/bridge; frontend/src/state`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/035-react-bridge-state-resync.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The initial request does not lose an event that arrived at the same time.
- [ ] A repeated mount does not multiply subscriptions.
- [ ] The test adapter is not called evidence of system integration.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] A Go restart with a new `runtimeID` and a lower revision resynchronizes the UI; an old message does not bring the previous library back on screen.
- [ ] A sequence of full snapshots with skipped numbers does not create an avalanche of requests; a broken delta sequence causes one controlled resync.

## Checks

- Check duplicated, reordered and missing events.
- Check a late initial snapshot, a reload and a command error.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

- Simulate a new Go launch, a late response to an old initial request, jumps in full snapshots and a lost operation result. Check the bounded resync requests and the absence of a repeated mutation.

## Decisions, risks and boundaries

The test bridge and browser checks speed up the UI, but do not prove system callbacks, dialogs, network transfer or application shutdown.

**Basis for technical decisions:** [React effects](https://react.dev/reference/react/useEffect), [External store](https://react.dev/reference/react/useSyncExternalStore). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): the ordering rules are `frontend/src/bridge/contract/ordering.ts`; while `needsFetch(state)` is true (no state, nothing fetched — e.g. after a failed first fetch) the store fetches with its own back-off; restoring operations after a reload needs the addition named in 027.

**From the 009 checkpoint** ([refactor after 009](../../docs/verification/refactor-after-009.md)): the gate has two pending steps, `engine integration` (018) and `test-bridge UI tests` (yours). Whichever of 018 and 035 replaces the last of them deletes the helper that builds pending steps and `notBuiltYet` in `internal/devtool/check.go` with it: uncalled code has zero-count blocks and fails the gate.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** a minimal correct implementation, tests and the required regression; **100% of all owned executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Write a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock it and the independent tasks that can continue.
- Update the documentation, the contract and the README if the API, structure or build commands changed; do not leave required context only in the chat.
