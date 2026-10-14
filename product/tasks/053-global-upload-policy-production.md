# 053 — Implement independent global upload disabling

**Status:** `planned`  
**Phase:** P5 — Working main scenario and full MVP  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [010](010-upload-stop-and-pause-spike.md), [025](025-pause-resume-application.md), [026](026-rate-limits-seed-network.md), [029](029-restore-recheck-reconciliation.md), [015](015-transactional-repositories.md)  
**Requirements:** `TR-08`, `SET-08`

## Outcome

Implement the proven global upload shutdown policy for existing, new and restored entries.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-08`, `SET-08`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): APIs and assumptions not yet confirmed by a prototype.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Apply the supported strategy that passed 010; if the check fails, the task is blocked.
2. Persist the global intent before broadcasting the policy; new entries receive the initial flags.
3. Return pending/confirmed/timeout based on the proven stop of payload, not on the setter returning.
4. After the normal mode is restored, keep manualPaused and wanted.
5. Reconcile repeated toggles by the latest revision; do not call a failure a success.
6. Do not substitute the independent upload shutdown with a full pause or a silent engine change.

## Artifacts and change locations

**Proposed locations:** `internal/app/global_upload_policy`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/053-global-upload-policy-production.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Download continues; upload of useful data stops within the proven bound.
- [ ] New and restored entries receive the policy before payload.
- [ ] A timeout is visible as an apply error.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check active peers with buffered data and new connections.
- Check completion/new import, manual pause and restart during a toggle.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

If 010 is blocked, this task is also blocked. A full pause of all torrents is not an independent upload shutdown. Changing the engine requires an explicit new decision.

**Basis for technical decisions:** [Upload implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/torrent.go). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

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
