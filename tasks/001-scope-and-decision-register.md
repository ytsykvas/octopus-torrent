# 001 — Record the initial scope and open decisions

**Status:** `done`  
**Phase:** P0 — Context and repository start  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** None — initial task.  
**Requirements:** `AI-06`, `DATA-01`, `QA-07`

## Outcome

Create a consistent baseline description of the project: agreed capabilities, sources of requirements, the location of future code and a list of questions that must be verified before the corresponding development stages.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-06`, `DATA-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Compare the four current specifications with the mockup. Compile a list of all 72 requirement IDs and 19 end-to-end scenarios; do not change their numbering. Record any contradictions found, with references to the sources.
2. Record the actual macOS version, Mac architecture, available disk space and installed development tools. Distinguish the verified configuration from the minimum supported macOS version, which is yet to be determined.
3. For each open question, specify the corresponding task, the verification method, the selection criterion and the document in which the decision will be stored. In particular, cover dependency versions, the SQLite driver, engine storage, restore and disabling upload.
4. Split the work into an interim verification of the main transfer, the full MVP and the agreed subsequent capabilities. Search, filters, collections and automation remain part of the overall plan, even if they are not included in the first MVP.
5. Review the possible directories for code and the existing files. Record the chosen directory, the documentation storage order and the boundaries of read-only files; do not create a second repository inside the existing one.

## Artifacts and change locations

**Proposed locations:** `docs/decisions/project-baseline.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** a `docs/verification/001-scope-and-decision-register.md` record or an exact reference to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] The requirements list contains all 72 IDs, and the scenario list contains all 19 scenarios; discrepancies between documents are recorded explicitly.
- [x] Each open decision has a task, a selection criterion and the required verification.
- [x] The baseline description confirms a personal local application without sign-in, payments, a cloud service or a built-in AI feature.
- [x] The code directory and the next repository setup task are defined; the existing files and read-only sources are preserved.
- [x] Verified facts are separated from assumptions and future checks.

## Checks

- Manually compare the list of IDs and scenarios with the current requirements; record the count and any discrepancies.
- For each item in the open parameters section, find the corresponding task and its completion criterion.
- Review the file changes and make sure the read-only sources have not been modified.

**How to verify:** perform this manual review and record the actual result. The commands from the future task [004](004-reproducible-check-commands.md) are not required to complete 001.

## Decisions, risks and boundaries

Mark unverified decisions as proposed or pending verification. This task organizes the already agreed project: it does not change the stack, does not remove subsequent capabilities and does not add new ones. If the boundaries of two documents contradict each other, record the contradiction and how it is resolved, rather than silently choosing the more convenient option.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** according to the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; rerun the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, state the concrete reason, the missing evidence/decision, how to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

### Execution result (14 October 2026)

- **Status:** `done`. Result — [project-baseline.md](../docs/decisions/project-baseline.md), evidence — [report 001](../docs/verification/001-scope-and-decision-register.md).
- **Discrepancy in the text of this task:** "72 IDs" is an outdated figure from revision 2; the current number is **80** (QA-01–QA-08 were added). The inventory was verified for 80 IDs; see R1 in the baseline. The numbering was not changed.
- **Changed:** copied the documentation into `/Users/tsykvas/projects/torrent`; corrected the mockup name in the requirements (R2); updated `docs/README.md`.
- **Checks:** count of IDs/scenarios, matrix reconciliation, link and anchor check — `pass`; code coverage — `not_applicable`.
- **Limitations:** Go and the Wails CLI are not installed — a prerequisite for 003. The minimum macOS version is open (O1).
- **Next task:** [002](002-repository-and-agent-entrypoint.md).
