# 007 — Check the anacrolix API and lifecycle on a single version

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [006](006-controlled-torrent-fixtures.md), [005](005-domain-dto-and-bridge-contract.md)  
**Requirements:** `AI-05`, `AI-07`, `STACK-03`

## Outcome

Pin a single version of anacrolix/torrent and verify the creation, operation and shutdown of the engine objects the application needs.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-05`, `AI-07`, `STACK-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): the API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Pin the anacrolix/torrent version tag in the dependencies and check the signatures of all required methods against it.
2. On controlled torrents, verify `NewClient`, `AddTorrentSpec`, `GotInfo`, `Stats`, `Drop`, `Closed` and `Close`; record the results and errors of each operation.
3. Build the program on the target Mac and describe the actual dependencies on network transports, system libraries and CGO.
4. Create and shut down the client several times with an active torrent and a torrent awaiting metadata; verify that the associated background tasks finish.
5. Describe when an import manages a new engine object and when it receives the object of an already existing torrent; define which resources each of these scenarios is allowed to release.

6. Build the smallest end-to-end prototype on the results of 003/005/006: a controlled torrent transfers real bytes, Go returns its own DTO, and React in a real Wails window shows the progress change. SQLite, the full preview, collections or the finished design are not needed yet. The experiment is isolated from the product UI; its temporary commands do not end up in the personal working build.
7. Change the state of the controlled transfer, verify the display through bindings and events, reload the UI and quit the `.app`. Record what has been proven at this boundary; reuse the network test materials in 018/034 rather than maintaining a second working engine.

## Artifacts and change locations

**Proposed locations:** `docs/decisions/engine-version.md; docs/verification/engine-lifecycle.md`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/007-engine-api-lifecycle-spike.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] All required calls compile and run on a single pinned engine version.
- [ ] The client and the metadata-waiting tasks shut down without hanging and without leftover background tasks.
- [ ] The report separates the verified lifecycle from the not-yet-proven stopping of upload and data restore.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not run or failed are not marked as successful.

- [ ] The early prototype shows a real transfer through Go → Wails → React; static data or a browser substitute does not count. Closing the prototype shuts down its own torrent client and leaves no active transfer.

## Checks

- Repeat the client shutdown, including during metadata retrieval; the program must exit without hanging or releasing resources twice.
- Reproduce a failed add and a `new=false` return: the error must be explicit, and the existing torrent must continue to work. Run these scenarios with data race detection in the supported configuration.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

- Match the change in the controlled client's counters with the DTO and the display in the `.app`; verify that subscriptions are cleaned up after remounting. After closing, verify that only this spike's resources are released. This is evidence of the integration path, not of restore from SQLite or of the full MVP.

## Decisions, risks and boundaries

The application must exchange its own DTOs through the engine adapter and perform persistence through repositories. Returning control from a method does not prove that the whole operation has finished. Record the completion contracts and resource management rights in an ADR together with the verification results.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, check the signatures and constraints against the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
