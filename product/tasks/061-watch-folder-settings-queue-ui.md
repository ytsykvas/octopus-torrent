# 061 — Configure preview and paused automatic adding

**Status:** `planned`  
**Phase:** P6 — Library and local automation  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [060](060-watch-folder-stable-detection.md), [059](059-rules-crud-import-application.md), [044](044-import-preview-ui-tree.md), [050](050-settings-ui-save-feedback.md)  
**Requirements:** `AUTO-01`, `AUTO-02`

## Outcome

Add watch settings with preview by default and an explicit paused auto-add.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AUTO-01`, `AUTO-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Persist the watch path and a separate autoAdd flag, false by default.
2. Explain that the source is kept and the record is paused; show previews one at a time.
3. Reconcile cancellation, retry and queued candidates after a mode change.
4. Auto-add goes through the same rules/path/dedup/commit with manualPaused=true.
5. Candidate errors are visible without a storm of modal windows.
6. A vanished watched folder is not created or replaced silently.

## Artifacts and change locations

**Proposed locations:** `frontend/src/features/settings/watch; AutomationService`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/061-watch-folder-settings-queue-ui.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Automatic adding is enabled explicitly.
- [ ] A new automatic entry does not transfer payload until a manual start.
- [ ] The settings are kept after a restart.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Go through the watch part of AS-16.
- Check restart with auto enabled, a manual/watch duplicate and a problematic rule path.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Automatic adding does not mean automatic start: manualPaused=true is kept until transfer is allowed. Preview by default and explicit auto-add are separate settings.

**Basis for technical decisions:** [fsnotify FAQ](https://github.com/fsnotify/fsnotify#faq). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

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
