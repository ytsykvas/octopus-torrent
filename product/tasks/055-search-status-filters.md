# 055 — Add search and status filters

**Status:** `planned`  
**Phase:** P6 — Library and local automation  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [040](040-app-shell-fixed-geometry.md), [048](048-persistent-bulk-selection-ui.md), [027](027-metrics-status-snapshots.md)  
**Requirements:** `NEXT-01`, `NEXT-02`, `UI-07`, `UI-16`

## Outcome

Add local search by name and status filters without actions on a hidden selection.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `NEXT-01`, `NEXT-02`, `UI-07`, `UI-16`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Record the rules for active/downloading and the intersection of completed/seeding in an ADR.
2. Implement search by name with Unicode/case handling without a new content catalog.
3. Counters and filters use a single status contract.
4. Switching between sections scrolls the list back to the top; speed updates do not change the scroll position.
5. Before a group action, remove hidden UUIDs; select all covers only visible entries.
6. Define the behavior of the details and a shared pure filter for collections.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/library/filters; filter contract`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/055-search-status-filters.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Hidden entries do not receive a group command.
- [ ] An empty query returns the full list.
- [ ] The rules of each filter are described explicitly.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check a Unicode query and status changes.
- Check a hidden selected entry and AS-17 after filtering.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

NEXT-01/02 are implemented after the core scenario but remain in the final scope. Clarify the statuses "active/downloading/completed/seeding" with a table of selection rules.

## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
