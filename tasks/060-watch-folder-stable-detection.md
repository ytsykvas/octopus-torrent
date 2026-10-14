# 060 — Reliably watch for new .torrent files

**Status:** `planned`  
**Phase:** P6 — Library and local automation  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [028](028-application-startup-quit.md), [019](019-metainfo-parser-file-tree.md), [021](021-full-hash-deduplication.md), [020](020-destination-filesystem-service.md)  
**Requirements:** `AUTO-01`

## Outcome

Reliably detect completed .torrent files through the watcher, metadata checks and rescanning.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AUTO-01`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Agree on one local folder and a subfolder policy; watching is enabled explicitly.
2. Listen to Events/Errors and run a bounded scan after startup, restart or an error.
3. Measure the stability interval, parse the metadata and recheck the file state; support atomic rename.
4. Bound the queue and coalesce repeated events; dedup by full identity, and an error in one piece of content does not create a notification storm.
5. Do not move or delete the original .torrent files; the error list is logical only.
6. On folder change/quit, close the watcher and the candidates' contexts.

## Artifacts and change locations

**Proposed locations:** `internal/automation/watcher; candidate queue`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/060-watch-folder-stable-detection.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] A partially written file is not imported.
- [ ] A missed event is found by rescanning.
- [ ] Source files stay in place.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

## Checks

- Check a slow write and an atomic file replacement.
- Check a renamed folder, missed events, a burst of changes and shutdown.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

A watcher event does not prove that writing has finished. A stability interval, metadata parsing, a file recheck and a reconciling scan are needed together. The original .torrent stays in place.

**Basis for technical decisions:** [fsnotify FAQ](https://github.com/fsnotify/fsnotify#faq). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
