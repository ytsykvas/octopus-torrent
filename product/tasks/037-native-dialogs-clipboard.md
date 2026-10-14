# 037 — Implement native dialogs and full path copying

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [034](034-wails-facade-real-bindings.md), [020](020-destination-filesystem-service.md)  
**Requirements:** `ADD-01`, `ADD-04`, `ADD-08`, `SET-01`, `SET-03`, `SET-05`, `UI-12`

## Outcome

Provide system dialogs from Go and exact copying of the full path.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-01`, `ADD-04`, `ADD-08`, `SET-01`, `SET-03`, `SET-05`, `UI-12`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Invoke the system file and folder selection dialogs through Go; do not rely on the browser `File.path`.
2. Distinguish a successful selection, a cancellation and an error; re-validate the received path in Go.
3. Create a named folder through 020 after explicit confirmation. A finished service for locating missing data is not required for this.
4. Copy the real full value, not the visually shortened text; verify Unicode and the clipboard.
5. Verify the initial folder, focus return and reopening of the system dialog. Integration of re-selecting the location belongs to 030/047 and is verified there.

## Artifacts and change locations

**Proposed locations:** `internal/platform/dialogs; clipboard adapter`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** a `docs/verification/037-native-dialogs-clipboard.md` record or an exact reference to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Cancellation does not change the path or the settings.
- [ ] A Unicode path is copied without truncation.
- [ ] Folder selection and creation work before path recovery is implemented; the dialog does not move files or change settings on its own.
- [ ] The required checks have been performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Verify cancellation, a wrong type and missing permissions.
- Check the actual clipboard, an existing folder and focus after a child dialog.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, of the test UI and of system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undetermined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Wails dialogs](https://wails.io/docs/reference/runtime/dialog/). Check the signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
