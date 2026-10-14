# 025 — Implement torrent start and manual pause

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [023](023-durable-import-commit.md), [013](013-transfer-policy-state-machine.md), [018](018-torrent-engine-adapter.md)  
**Requirements:** `TR-01`, `STACK-03`

## Outcome

Persist the user's decision to start or pause, and separately return the actual result of applying that policy in the engine.

## Read before starting

- [Task execution rules](README.md): start and completion conditions, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-01`, `STACK-03`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If required evidence is missing, the dependent part of the work must not start.

## Work order

1. Before applying the command, record `manualPaused` and the operation details.
2. While paused, forbid download and upload of file data; separate them from protocol service traffic.
3. On resume, compute the effective permissions taking the global upload policy into account.
4. Through the defined command order, report the pending-application state and an explicit error if the command did not execute.
5. Protect the current revision and the deletion state from late acknowledgements of old commands.

## Artifacts and change locations

**Proposed locations:** `internal/app/torrent_service.go`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/025-pause-resume-application.md` or an exact link to an existing verification or build report in the handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A manual pause persists after an application restart.
- [ ] Resuming a torrent does not cancel a global upload ban.
- [ ] A failure to save or apply the policy is not reported as success.
- [ ] All checks below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- In a controlled group of clients, measure data transfer after a pause; both directions must stop according to the verified criterion, and service traffic is accounted for separately.
- Rapidly alternate commands, cause a DB error and a torrent removal between the command and its acknowledgement; the current policy must persist and the removed record must not be restored.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Commands are passed through our own DTOs and applied sequentially by the service; the policy is written through the repository. An accepted command is not confirmation that file data has stopped. The application result, the error and the current revision must remain separate and verifiable.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADRs, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
