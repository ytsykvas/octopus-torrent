# 002 — Prepare the Git repository and the AI entry point

**Status:** `done`  
**Phase:** P0 — Context and repository start  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [001](001-scope-and-decision-register.md)  
**Requirements:** `AI-01`, `AI-02`, `AI-03`, `AI-04`, `AI-09`, `QA-01`, `QA-03`, `QA-04`, `QA-07`, `QA-08`

## Outcome

Prepare a Git repository and documentation with which a new AI agent can find the current requirements, understand module boundaries and continue with the next task without the chat history.

## Read before starting

- [Task execution rules](README.md): conditions for starting and finishing a task, statuses and verification evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-01`, `AI-02`, `AI-03`, `AI-04`, `AI-09`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- The results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Check the chosen directory, an existing Git repository and uncommitted changes. Use a suitable existing repository or initialize a new one in the agreed location; keep the Markdown documents, the mockup and other people's changes.
2. Create a root README with the current development state, the chosen stack, links to the specifications and tasks, and a map of future modules. Clearly mark which directories and commands do not exist yet.
3. Create AGENTS.md with the boundaries of React, Go services, the engine adapter and the repository, rules for working with files and generated bindings. Describe how to find a task's criteria, store decisions and hand the result off to the next agent.
4. Configure .gitignore for build outputs, working databases, temporary files and local logs. Do not hide required source files or safe test materials; do not add personal torrents and downloaded data to the repository.
5. Create a short ADR template: question, decision, alternatives considered, verified grounds and consequences. Add links to the initial decisions from 001.

6. Keep and refine the already created AGENTS.md and docs/: the engineering policy, the index, the feature template, the locations of decisions and verification. Do not overwrite them with an empty scaffold. Provide for architecture analysis, TDD, 100% coverage and debt removal in every session; feature documentation starts before the code.

## Artifacts and change locations

**Proposed locations:** `README.md; AGENTS.md; .gitignore; docs/decisions/`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/002-repository-and-agent-entrypoint.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] From the root README you can navigate to the current requirements, architecture, concept, the engine technical plan and the next task.
- [x] AGENTS.md explains module boundaries, working with generated code and the order of verifying the result without links to an unavailable chat.
- [x] The documentation distinguishes the future structure from files already created; commands that will be verified in 004 are not described as working.
- [x] For future scripts, the requirement of independence from the developer's absolute path is recorded.
- [x] The Git settings keep the required sources and do not include working databases, personal data or unrelated unplanned changes.

## Checks

- Navigate from the README to one requirement, the corresponding future module and the next task; fix missing or incorrect links.
- Check the Git state and the file diff: no personal paths, logs, downloaded data or accidentally changed third-party files.
- Check the ignore rules on examples of a working database, a build output and a test source file; the latter must remain available for storing in Git.

**How to check:** a manual review of the documentation and the repository state is enough. The future commands of task [004](004-reproducible-check-commands.md) are not a prerequisite of 002.

## Decisions, risks and boundaries

Do not turn AGENTS.md into a duplicate of all specifications: keep short rules and links to the sources. Do not initialize a nested Git repository and do not discard existing changes. This task prepares a place for code; whether the stack and commands work is checked in the following tasks.

**Basis for technical decisions:** [Go modules](https://go.dev/doc/modules/managing-dependencies). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.


## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the plan of the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run a test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** a minimal correct implementation, tests and the required regression; **100% of all owned executable code** by the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; the absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Write a short result: what was changed, the exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock it and the independent tasks that can continue.
- Update the documentation, the contract and the README if the API, structure or build commands changed; do not leave required context only in the chat.

### Execution result (14 October 2026)

- **Status:** `done`. Evidence: [report 002](../docs/verification/002-repository-and-agent-entrypoint.md).
- **Created:** a Git repository (branch `main`) at the root `/Users/tsykvas/projects/torrent`; [README.md](../README.md); [.gitignore](../.gitignore); [ADR template](../docs/decisions/_ADR_TEMPLATE.md). **Extended:** [AGENTS.md](../AGENTS.md) (module boundaries, file operations, bindings, task handoff), [docs/README.md](../docs/README.md).
- **Checks:** `.gitignore` on 35 sample real files; all relative links and anchors; the route README → requirement → task → module; the Git index without databases, logs and personal data: `pass`. Code coverage: `not_applicable`.
- **Limitations:** build and check commands do not exist yet (003/004). This session's link-check and `.gitignore` scripts were not added to the repository: the permanent documentation check is created by 004.
- **Next task:** [003](003-wails-macos-build-spike.md); install Go and Wails CLI before it.
