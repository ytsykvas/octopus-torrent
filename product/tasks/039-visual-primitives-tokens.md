# 039 — Port design tokens and base components

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [003](003-wails-macos-build-spike.md), [005](005-domain-dto-and-bridge-contract.md)  
**Requirements:** `UI-06`, `UI-04`, `UI-15`, `AI-03`, `QA-01`, `QA-02`, `QA-03`

## Outcome

Reproduce the mockup's styling with small React components.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-06`, `UI-04`, `UI-15`, `AI-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Extract colors, spacing, typography and radii for the light, dark and opaque appearance.
2. Create Button/IconButton/Badge/Progress/Ellipsis/Tooltip/Toast with explicit focus, disabled and error states.
3. Supplement the direction, pause, start, delete and collapse icons with accessible names.
4. Show names as text; do not port the mockup's innerHTML templates.
5. Check compactness and hit areas without excessive universal configuration.

## Artifacts and change locations

**Proposed locations:** `frontend/src/styles; frontend/src/components`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/039-visual-primitives-tokens.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A long translation does not break controls.
- [ ] Focus, disabled and error states are readable in all themes.
- [ ] A name that looks like HTML does not become markup.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called passed.

## Checks

- Visually check keyboard focus and text scaling.
- Check long translations and untrusted names.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

- First a component/interaction test or a verifiable visual regression of the expected change, then the implementation. Visual review supplements TDD and 100% of our own TS code; reusable components are not copied for each screen.

## Decisions, risks and boundaries

Stay within the boundaries of our own DTOs, repository and engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Apple Materials](https://developer.apple.com/design/human-interface-guidelines/materials). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

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
