# 018 — Create a torrent engine adapter with explicit resource management

**Status:** `planned`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [007](007-engine-api-lifecycle-spike.md), [005](005-domain-dto-and-bridge-contract.md), [013](013-transfer-policy-state-machine.md)  
**Requirements:** `AI-03`, `AI-04`, `AI-05`, `STACK-01`, `QA-03`, `QA-08`

## Outcome

Isolate anacrolix/torrent in a single adapter and define which objects and background tasks it creates, uses and shuts down.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-03`, `AI-04`, `AI-05`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Define our own contracts `Add`, `GetMetadata`, `ApplyPolicy`, `ReadSnapshot`, `Drop` and `Close`, including errors and cancellation.
2. Link record UUIDs to engine objects and distinguish objects created for the current import from already existing ones that this import does not manage.
3. Pass storage, clock and event receiver explicitly; return copies of state snapshots, not mutable engine objects.
4. Manage cancellation at the level of the application and of an individual torrent, and return resource shutdown errors.
5. Create a test double of the adapter for testing services and a real implementation for controlled network tests.

7. Check the existing shared contracts and pure policies before adding new methods. Remove duplicates and unnecessary dependencies in the adapter on green tests; every new error and resource-shutdown branch has a behavioral test and is part of the 100% coverage.

## Artifacts and change locations

**Proposed locations:** `internal/torrentengine/engine.go; anacrolix_engine.go`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/018-torrent-engine-adapter.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Engine objects are not passed across the Go–TypeScript boundary.
- [ ] A request made before metadata is obtained returns a defined pending state, not a falsely ready file list.
- [ ] A late call for a removed UUID is ignored and does not restore the record.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Simultaneously read a snapshot and remove a torrent, and also shut down the client while adding; operations must finish without races, hangs or access to released resources.
- Check `new=false`: cancelling a new import must not remove an already existing torrent. Run the concurrent scenarios with data race detection.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Services receive the adapter through our own contract and do not access library objects directly. The right to shut down an object is determined by who manages it, not merely by holding a reference to it. An accepted command and a completed operation have different results.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
