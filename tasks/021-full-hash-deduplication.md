# 021 — Avoid duplicates by full torrent hashes

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [019](019-metainfo-parser-file-tree.md), [015](015-transactional-repositories.md)  
**Requirements:** `ADD-03`, `STACK-03`, `SET-02`

## Outcome

Detect duplicates by full torrent identities, including when several imports run concurrently.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-03`, `STACK-03`, `SET-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Outcomes and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Store the library UUID separately from the full v1 SHA-1 and v2 SHA-256 hashes.
2. Reserve the identity in a transaction and re-check it after the metadata is obtained.
3. Match the identities of a hybrid torrent without merging incompatible file structures.
4. For a duplicate, return `existingUUID` so that the interface opens the existing record; do not change its path, file selection or `manualPaused`.
5. Do not call `Drop` on the object of an existing torrent that the import does not manage; do not use the name or size as identity.

## Artifacts and change locations

**Proposed locations:** `internal/app/identity; internal/library identity repository`. This is a map of future code; after the project skeleton is created, check the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/021-full-hash-deduplication.md` or an exact link to an existing verification or build report in the work handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Concurrently adding the same torrent from different sources creates only one record.
- [ ] Metadata obtained later does not cause a duplicate to appear.
- [ ] A repeated import does not change the settings of the existing record.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as passed.

## Checks

- Add a v1 torrent first and then the corresponding hybrid torrent; repeat for v2 and for two concurrent confirmations. One compatible record must remain in the library.
- Add different torrents with the same names and reproduce a uniqueness constraint conflict; different data must not be merged, and the failed transaction must be rolled back.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label UI checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

The full hash and the library UUID play different roles. A short name, size or abbreviated hash representation does not prove identity. A conflict detected after the metadata is obtained must produce an explicit result and must not damage the existing record.

**Basis for technical decisions:** [BEP52](https://www.bittorrent.org/beps/bep_0052.html), [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). During implementation, reconcile signatures and constraints with the pinned version. Reading the API documentation does not replace a performed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short outcome: what changed, exact checks with statuses `pass`, `fail` or `not_run`, ADR, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
