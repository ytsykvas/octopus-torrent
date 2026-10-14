# 067 — Write local help and the recovery procedure

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [066](066-personal-build-install-license-audit.md), [062](062-automation-end-to-end-gate.md), [029](029-restore-recheck-reconciliation.md)  
**Requirements:** `REL-03`, `AI-06`, `AI-09`, `QA-04`

## Outcome

Write clear local help and the actual upgrade and recovery procedure.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `REL-03`, `AI-06`, `AI-09`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Explain import, wanted/manual pause, global upload and the default folder.
2. Describe the consequences of deletion, tags/collections/rules/watch paused/source retention.
3. Show appdata/logs and recovery by codes without a blind reset.
4. Record the verified build/install/upgrade backup/compatible rollback commands.
5. Keep known limits/evidence; localize our own help.
6. Update the README and the contracts together with the procedure.

7. Reconcile the user help with the already maintained docs/features/. Do not postpone the internal feature descriptions until this late task: fix discrepancies and confirm that the current documents were created together with the corresponding changes.

## Artifacts and change locations

**Proposed locations:** `docs/help; README.md; docs/maintenance`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/067-owner-help-upgrade-recovery-runbook.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The help matches the real .app.
- [ ] There are no hidden setup steps.
- [ ] Recovery keeps the original data.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Go through AS-01/04/05/16/18 with the owner.
- Check links/commands and the instructions for migration failure/rollback.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of own DTOs, the repository and the engine adapter. An error has an explicit result; accepting a command does not mean the operation has completed. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

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
