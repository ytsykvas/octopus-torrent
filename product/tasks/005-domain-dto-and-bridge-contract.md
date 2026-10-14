# 005 — Describe our own Go–TS contracts and errors

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [003](003-wails-macos-build-spike.md)  
**Requirements:** `AI-05`, `AI-02`, `STACK-01`, `STACK-02`, `STACK-03`

## Outcome

Describe and verify our own contract between Go and TypeScript: data structures, commands, errors, revisions and the operation lifecycle. It must allow React and Go to be developed against shared examples without passing internal engine objects.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-05`, `AI-02`, `STACK-01`, `STACK-02`, `STACK-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Define the identifiers UUID, full v1/v2 infohash, file identifiers and indexes, `pendingImportID`, `operationID` and `revision`. For each field specify the format, units of measurement, allowed limits and whether the value may be absent.
2. Separate command acceptance from operation completion, and stored desired policy from actual engine state. Describe success, error, cancellation and request retry for each type of non-trivial operation.
3. Create Go DTOs for the library, torrent, files, preview and settings. For `AppError` define a stable code, localization parameters and operation or torrent identifiers; do not make a technical error ready-made UI text.
4. For integers outside the JavaScript safe range, define a decimal-string representation. Verify serialization in Go and reading in TypeScript on shared examples, including boundary values.
5. Describe the initial full state snapshot, subscriptions, discarding of stale messages and recovery after a missed update. Distinguish full snapshots from partial events so that intentional merging of updates does not cause endless repeated requests.
6. Generate the Wails bindings and create our own bridge wrapper. Add DTO structure checks and examples of a repeated `operationID`, cancellation and an unknown error code; the actual service behavior will be verified by the dependent tasks.

7. Add to snapshots and events a `runtimeID` — a new temporary identity for each Go run — and a revision within that run. This is not an installation identifier and not analytics. Define how a new connection gets the authoritative full state and how late responses of a previous connection are discarded. A full snapshot may skip numbers because updates are merged; a partial event requires the matching base revision.
8. For a repeated `operationID`, define sameness of the normalized request, storage of the result of non-trivial mutations, and rejection of the same ID with a different request. Completion must be available through a repeated request or the full state; a one-off event is not the sole evidence of success.

## Artifacts and change locations

**Proposed locations:** `internal/app/contracts; frontend/src/bridge; docs/decisions/api-contract.md`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/005-domain-dto-and-bridge-contract.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Go and TypeScript in strict mode read shared DTO examples with the same field meaning and units of measurement.
- [ ] Values beyond `Number.MAX_SAFE_INTEGER` pass through the chosen contract without loss of precision; a `null` ETA and a zero size have an unambiguous meaning.
- [ ] The contract distinguishes an accepted command from a completed operation and defines the response to a repeated `operationID` and to cancellation.
- [ ] An understandable fallback message is provided for an unknown error code, and an invalid DTO is rejected with a defined result.
- [ ] Generated bindings are separated from our own bridge; the contract contains no channels, pointers or mutable anacrolix/torrent objects.

- [ ] A new run with revision 0 is accepted as a new state; a late event of the old run does not replace it. A revision jump in a full snapshot does not cause endless repeated requests.
- [ ] Repeating the same `operationID` with the same request does not perform the mutation a second time; a different request with this ID is rejected.

## Checks

- Check serialization and reading of the shared examples: zero bytes, zero total size, missing ETA, the largest JavaScript safe integer and the first number beyond that limit.
- Check validation of missing required fields, an unknown status, an invalid file index and an unknown error code.
- Using the contract examples, check the expected responses to a repeated `operationID`, cancellation and a stale revision. Do not call the example check evidence of already implemented command serialization in the service.
- Run bindings generation, Go compilation and the TypeScript check; record the commands used. If the 004 suite is not ready yet, the verified commands from 003, supplemented with this task's commands, are sufficient.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) once they exist, or record your own exact commands for this contract. 004 is not a hard dependency of 005. Record results of contract tests, the test bridge and the real service separately.

- Check a `runtimeID` change, revision 0 after a previous large revision, an old response to the initial request and merged full snapshots. Lose the operation completion message and get its result through a repeated request.

## Decisions, risks and boundaries

Wails generates bindings, but does not define our units of measurement, command order, operation retry or recovery of missed events. These rules must be described explicitly and implemented in the following tasks. Do not introduce a second source of business state in React; record contract changes in an ADR together with compatibility examples.

**Basis for technical decisions:** [TypeScript strict](https://www.typescriptlang.org/tsconfig/strict.html), [Wails events](https://wails.io/docs/reference/runtime/events/). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.


## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
