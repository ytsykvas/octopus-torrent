# 026 — Implement global limits, seeding and network recovery

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [025](025-pause-resume-application.md), [015](015-transactional-repositories.md)  
**Requirements:** `TR-03`, `TR-04`, `TR-05`

## Outcome

Implement global speed limits, seeding after completion and network recovery while preserving manual pauses.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-03`, `TR-04`, `TR-05`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Create the rate-limiting objects before the client starts; set the unlimited mode explicitly.
2. Validate positive values and units. Change limits through the supported thread-safe API, not by directly modifying the client configuration.
3. Choose the policy for seeding completed selected data; do not use the Seeding() method as the only source of status.
4. Distinguish absence of network, absence of transfer participants and tracker errors. Restoring the connection does not create a new client.
5. Persist settings and do not start manually stopped entries after the network returns.

## Artifacts and change locations

**Proposed locations:** `internal/torrentengine/limits; internal/app/preferences`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/026-rate-limits-seed-network.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A completed torrent can transfer data to a new participant.
- [ ] The global limit applies to several simultaneous torrents.
- [ ] A zero limit value does not substitute for globally disabling upload.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Measure the total speed of several torrents with a positive limit and without one; account for short burst spikes.
- Cut the network, bring it back and check an active and a manually stopped entry.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of own DTO, repository and engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Client config](https://raw.githubusercontent.com/anacrolix/torrent/master/config.go). Cross-check signatures and limitations against the pinned version; reading the API is not a passed integration check.

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
