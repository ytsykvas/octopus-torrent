# 013 — Describe torrent transfer states and policy

**Status:** `planned`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [005](005-domain-dto-and-bridge-contract.md), [007](007-engine-api-lifecycle-spike.md)  
**Requirements:** `TR-01`, `TR-03`, `STACK-03`, `AI-04`

## Outcome

Create a verifiable policy table and define a sequential order of commands that change a single torrent.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `TR-01`, `TR-03`, `STACK-03`, `AI-04`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): APIs and assumptions not yet confirmed by a prototype.
- The results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not be started.

## Work order

1. Build a decision table for manual pause, the global upload ban, file selection, metadata fetching, errors, removal and program shutdown.
2. Compute download and upload permissions independently; separate the desired policy from the actually observed transfer.
3. Define the command order for each entry and the way a new revision of the global policy is applied to all entries.
4. Describe the priorities of displayed states, including data checking, absence of selected files, completion and seeding.
5. Once removal has started, forbid restarting the entry and discard late acknowledgements and calls that could resume it.

## Artifacts and change locations

**Proposed locations:** `internal/app/transfer_policy.go; command dispatcher`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** an entry `docs/verification/013-transfer-policy-state-machine.md` or an exact link to an existing verification or build report in the handoff summary. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Resuming a torrent does not cancel the global upload ban.
- [ ] Re-enabling global upload does not start manually paused entries.
- [ ] The policy computation functions are tested without Wails, disk and network.
- [ ] All checks listed below have been performed at the appropriate level; checks that were not performed or failed are not marked as successful.

## Checks

- Check every valid row of the decision table and make sure the computed permissions match the stored policies.
- Reproduce different orders of concurrent commands and a late response after removal; the newer revision must be kept, and the removed entry must not reappear.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and in the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The `wanted` field describes whether a file is selected for download. It is not a permission or a ban on seeding a specific file: the global upload policy is computed separately.

**Basis for technical decisions:** [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** a minimal correct implementation, tests and the required regression; **100% of all owned executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, the actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock it and the independent tasks that can continue.
- Update the documentation, the contract and the README if the API, structure or build commands changed; do not leave required context only in the chat.
