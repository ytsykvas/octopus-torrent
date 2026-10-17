# 016 — Protect the library from migration errors and DB corruption

**Status:** `planned`  
**Phase:** P2 — Types, data and service boundaries  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [015](015-transactional-repositories.md)  
**Requirements:** `SET-02`, `DATA-01`, `STACK-04`

## Outcome

Preserve the user's library and policies on a migration failure or DB corruption by providing verified recovery.

## Read before starting

- [Task execution rules](README.md): start and completion conditions, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-02`, `DATA-01`, `STACK-04`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not yet confirmed by a prototype.
- Results and ADRs of all mandatory prerequisite tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Choose a way to create a consistent backup before a migration; do not copy the active DB file without accounting for the WAL journal.
2. Record the origin of the backup and define retention periods only for the application's own backups of its local data.
3. Verify the backup with `integrity_check`, `foreign_key_check` and the library's own invariants.
4. For a corrupted DB or a newer schema, show a recovery error without automatically replacing it with an empty DB.
5. Describe a compatible order for recovery and migration rollback; do not add a separate user interface for backup or export.

## Artifacts and change locations

**Proposed locations:** `internal/library/recovery; docs/decisions/database-recovery.md`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** the record `docs/verification/016-migration-backup-corruption-recovery.md` or an exact link to an existing check or build report in the handoff summary. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] On an error, the original DB and the torrent files remain on disk.
- [ ] Verified recovery returns the stored policies, including manual pauses.
- [ ] An unusable or inconsistent backup is not marked as valid.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Create a backup during an active write and interrupt the migration; the recovered DB must be consistent and the original data preserved.
- Check a truncated DB file and a backup directory without write permission; show an explicit error. After a successful recovery, make sure the manual pause has not changed.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until those commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; label interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

This is internal data protection during migrations, not a new backup or export feature. The active DB file alone, without the committed changes from the WAL, may not be a consistent copy; the copying method must be proven separately.

**Basis for technical decisions:** [SQLite backup](https://www.sqlite.org/backup.html), [PRAGMA](https://www.sqlite.org/pragma.html#pragma_integrity_check). During implementation, check signatures and constraints against the pinned version. Reading API documentation does not replace a completed integration check.

**From 008** ([sqlite-driver](../../docs/decisions/sqlite-driver.md)): `VACUUM INTO` on the pinned driver gives a copy that passes `integrity_check` (measured in the probe), and the driver also exposes SQLite's online backup API (`NewBackup`/`NewRestore` on its connection). `library.Close` may leave a WAL behind (`ErrBusy` while another process reads, `ErrFull` on a full disk): it holds committed changes and must never be deleted. A damaged page and a file that is not a database already arrive as `ErrCorrupt` without touching the file, and so does an empty or missing main file beside a WAL with commits (SQLite alone would delete that WAL). A restored or imported database file chosen by the owner is untrusted: open it with the driver's `_defensive=1` and `trusted_schema=OFF`, check `application_id`, then `integrity_check`.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Write a short result: what changed, exact checks with the statuses `pass`, `fail` or `not_run`, ADRs, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
