# 068 — Close all requirements and hand off the project without chat

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [067](067-owner-help-upgrade-recovery-runbook.md), [063](063-failure-race-fuzz-regression.md), [064](064-scale-profiling-lifecycle-soak.md), [065](065-native-accessibility-language-gate.md), [066](066-personal-build-install-license-audit.md)  
**Requirements:** `AI-10`, `AI-01`, `AI-09`, `AI-08`, `QA-01`, `QA-02`, `QA-03`, `QA-04`, `QA-05`, `QA-06`, `QA-07`, `QA-08`

## Outcome

Close the 72 requirements and 19 scenarios and hand off the repository without dependence on the chat.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-10`, `AI-01`, `AI-09`, `AI-08`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Walk through all 19 scenarios of the real .app with proper evidence at the engine/DB/UI/native boundary.
2. For each ID, link the primary task and the actual evidence; blocked/not_run is not done.
3. With a clean context, find the entry point, module and requirement and run build/check.
4. Trace UI→Go→engine→SQLite and add all missing explanations to the repository.
5. Record the package, tools, commands, boundaries and unresolved questions.
6. Update the status in the task files and README consistently; hand over the result to the owner in plain language.

## Artifacts and change locations

**Proposed locations:** `docs/verification/final-acceptance.md; tasks status updates`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/068-full-acceptance-clean-context-handoff.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Completion is possible only after all 72 requirements and 19 scenarios have actually passed.
- [ ] A new agent does not need the chat history.
- [ ] A blocked agreed requirement leaves the project incomplete.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] There is TDD evidence, exact 100% of own executable code, a full inventory, documentation of every implemented feature and verified architecture conclusions; no workarounds or technical debt are left.
- [ ] At the start of a session, a new agent follows AGENTS.md and plans the architectural impact and reuse before the change; deferred tests/documents or quality are not accepted as a completed result.

## Checks

- Walk through AS-01–19 for the final time.
- Check a clean checkout, tracing using only the repo, statuses, coverage and all links.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Respect the boundaries of own DTO, repository and engine adapter. An error has an explicit result; accepting a command does not mean the operation is complete. Do not add new product capabilities covertly. Record contract changes and undefined parameters in an ADR together with the implementation.

**Basis for technical decisions:** [Wails: how an application is structured](https://wails.io/docs/howdoesitwork/). Cross-check signatures and limitations against the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
