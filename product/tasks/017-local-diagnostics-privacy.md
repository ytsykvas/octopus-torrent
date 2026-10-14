# 017 — Create local diagnostics with private data protection

**Status:** `planned`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [005](005-domain-dto-and-bridge-contract.md), [004](004-reproducible-check-commands.md)  
**Requirements:** `DATA-02`, `DATA-03`

## Outcome

Create size-limited local logs that help find errors without leaking personal data.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `DATA-02`, `DATA-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If required evidence is missing, the dependent part of the work must not start.

## Work order

1. Record the time, category, error code and `operationID`; limit the size and number of log files and describe their rotation.
2. Before writing, redact access keys and credentials in tracker URLs, magnet links, full local paths and private names.
3. Separate the localized `AppError` from the technical description of the execution flow.
4. Handle logging errors without endless recursive logging and without changing the library.
5. Describe only manual local collection of diagnostics; do not add automatic sending or analytics.

## Artifacts and change locations

**Proposed locations:** `internal/diagnostics; docs/decisions/diagnostics.md`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** a `docs/verification/017-local-diagnostics-privacy.md` record or an exact reference to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Artificial secrets from test data are absent from the regular log.
- [ ] Logs do not exceed the defined limits and are available without a network.
- [ ] Showing and logging an error do not change torrent files.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Check a set of URLs, paths, names and secrets and trigger rotation; secret values must be redacted and storage limits respected.
- Simultaneously trigger logging and DB errors, and also terminate the program; it must not loop, lose the library or hide the root cause of the error.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

Diagnostics stay local and do not become a channel for automatically sending data. A log error must not change the result of the main operation or hide its error. Redaction rules for private values apply to writing to disk.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, the exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual code change locations and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
