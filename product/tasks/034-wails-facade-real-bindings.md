# 034 — Connect Go services to real Wails bindings

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [027](027-metrics-status-snapshots.md), [028](028-application-startup-quit.md)  
**Requirements:** `STACK-01`, `AI-05`

## Outcome

Connect the narrow Go API to the real Wails bindings.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `STACK-01`, `AI-05`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Connect the already implemented library and settings reads, import prepare/confirm/cancel, start/pause and state snapshots to the narrow Go facade. Validation, SQL and file rules stay in Go.
2. Generate the TypeScript models and calls; hide the generated paths behind our own adapter. A function existing in the test adapter does not mean a real Go method exists.
3. Verify the Wails startup context, cancellation, operation acceptance and completion, and stable error codes at the real Go–TS boundary.
4. Reconcile shared JSON examples, large numbers, `null`, `runtimeID`, revisions and a repeated `operationID`.
5. In the built `.app`, perform a real request, command, event and repeated snapshot fetch. Reloading the interface does not create a second torrent client.
6. Connect the bulk action, path restore and deletion methods during the integration of the corresponding features. Starting this base facade does not require 030/033 to be completed; do not substitute unfinished capabilities with successful stubs.

## Artifacts and change locations

**Proposed locations:** `internal/app/app_api; frontend/wailsjs; bridge contracts`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/034-wails-facade-real-bindings.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Go validates even an invalid UI request.
- [ ] Bindings are reproduced by generation without manual edits.
- [ ] Real serialization matches the test contract.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] Basic import, transfer and state reads work without finished bulk deletion and path restore; missing methods do not return a fake success.

## Checks

- Check an invalid DTO and the precision of large numbers.
- In Wails, observe operation acceptance and its actual completion separately.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

The facade grows together with the services. This task closes the base connection; 047/048/049 connect and verify their own additional commands. The API stays single, and the generated bindings are reproduced after every change. The mere future use of a service does not make its implementation a mandatory prerequisite of the whole facade.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): accept a `destination` only if it equals a folder Go got from the native dialog in this run or the saved default.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): delete the temporary engine spike: `cmd/engine-spike`, `internal/enginespike`, `frontend/src/spike`, `frontend/spike`, `frontend/vite.spike.config.ts` and the second half of `npm run build`, its `tsconfig`/`.oxlintrc`/`.gitignore` entries, its `wailsApps` row in `internal/devtool/appcov.go`, and [docs/tooling/engine-spike.md](../../docs/tooling/engine-spike.md). Keep the gate's `product isolation` step. The spike's page shows the pattern to reuse: subscribe before the snapshot, keep the highest revision.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
