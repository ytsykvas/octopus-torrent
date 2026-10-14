# 045 — Build cards, statuses and main actions

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [040](040-app-shell-fixed-geometry.md), [041](041-six-languages-formatters.md), [027](027-metrics-status-snapshots.md), [025](025-pause-resume-application.md)  
**Requirements:** `UI-01`, `UI-08`, `UI-13`, `UI-19`, `UI-12`, `UI-15`

## Outcome

The card shows an exact state snapshot and the result of real actions.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-01`, `UI-08`, `UI-13`, `UI-19`, `UI-12`, `UI-15`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Use stable UUID keys and separate controls for details, selection and collapsing, without nested buttons.
2. Show ↓ while downloading, ↑ while seeding and pause for stopped/completed transfer.
3. Download/seeding badges have the same color with different arrows; completed is red, paused is neutral.
4. Speeds are visible only during transfer; size and percentage sit near the end of the progress bar fill, with a clamped position.
5. Actions wait for Go confirmation; focus and access to the full text are preserved during updates.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/torrents/TorrentCard`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/045-torrent-cards-status-actions.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Paused/completed show no speed and no dash.
- [ ] Status, icon and buttons come from the same revision.
- [ ] Waiting for metadata, checking, error and no wanted files are not presented as completion.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check progress 0/1/99/100 and the download/pause/seeding transitions.
- Check long names, keyboard focus and frequent snapshots.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of our own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Wails events](https://wails.io/docs/reference/runtime/events/). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
