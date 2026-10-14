# 041 — Implement six languages and data formatting

**Status:** `planned`  
**Phase:** P4 — React UI and native integration  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [035](035-react-bridge-state-resync.md), [039](039-visual-primitives-tokens.md), [015](015-transactional-repositories.md)  
**Requirements:** `SET-06`, `UI-12`, `REL-03`

## Outcome

Implement complete dictionaries for six languages and precise data formatting.

## Read before starting

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `SET-06`, `UI-12`, `REL-03`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Create typed keys en/uk/es/de/fr/pt with consistent plurals and substitution parameters.
2. Get the order of preferred macOS languages through a verified system call, not only navigator.language.
3. Store the manual choice; changing the language updates dialogs and alerts without resetting selected files.
4. Translate AppError, accessible names, menus, help and our own dialog texts; do not translate data names.
5. Record the decision on decimal/binary units and rounding, the MB/GB rule and small files; do not lose precision beyond 2^53.
6. Mark the language limitations of dialog elements controlled by macOS.

## Artifacts and change locations

**Proposed locations:** `frontend/src/i18n; formatters; platform locale probe`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/041-six-languages-formatters.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] All our own texts exist in six languages.
- [ ] The manual choice is saved and applied without a restart.
- [ ] An unsupported language falls back to English.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called passed.

## Checks

- Check counts 0/1/2/many and substitution parameters.
- Check long German/French texts, Unicode names and large sizes.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

SET-06 covers texts controlled by the application. System dialog elements may have API language limitations; mark them explicitly, do not promise what does not exist.

**Basis for technical decisions:** [Transmission localization](https://github.com/transmission/transmission/blob/main/macosx/en.lproj/Localizable.strings). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.

This task creates the language infrastructure, base dictionaries and the missing-key check. Each subsequent UI feature adds its texts in six languages in its own task; 041 must not pretend it has already translated screens that do not exist yet. A full review of all screens is done in 065.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
