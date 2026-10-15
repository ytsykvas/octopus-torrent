# Mandatory engineering rules

**Status:** mandatory owner decision, 14 October 2026. Applies from the first change, including to spikes, fixes, refactoring and development tooling. Requirements QA-01–QA-08 are defined in the [specification](../../product/PROJECT_REQUIREMENTS.md). All planned tasks are subordinate to this document.

## 1. Start of every session: architecture and plan

Before making changes, the agent must:

1. Read [AGENTS.md](../../AGENTS.md), the current requirements, the [architecture](../../product/APPLICATION_ARCHITECTURE.md), the task with its prerequisites, the documentation of the affected feature and the latest changes in the repository. Establish the actual state of the code rather than relying only on the intent stated in documents.
2. Trace the scenario React → bridge/Wails → Go service → engine/SQLite/files. Identify the owner of the state, side effects, order of operations, errors, cancellation and safe release of resources.
3. Look for implementations that can be reused. Check for duplicated rules, wrong dependency direction, mixed responsibilities, hidden effects and unjustified complexity.
4. Write a short plan in the task report: which boundaries and contracts will change; what is reused; what must be refactored; which tests come first; how 100% coverage will be proven; which documentation will be updated. If no architecture change is needed, explain why in concrete terms.
5. Choose the simplest sufficient design within the agreed stack. Record a significant contract change in an ADR and in the architecture. A change of the stack or of agreed behavior requires an owner decision; an ordinary structural improvement within the task is carried out by the agent on its own.

This analysis happens at the start of **every session**, including when continuing the same task. It does not mean rewriting the whole project each time. The plan and conclusions may be kept in the existing task report; a separate formal document per session is not required.

## 2. TDD for every change

The **RED → GREEN → REFACTOR** cycle is mandatory:

1. **RED:** before changing the implementation, write or strengthen a test of the expected behavior and run it. Make sure it fails precisely because of the missing or incorrect behavior, not because of a broken environment. Record the test, the command and the reason for the failure.
2. **GREEN:** write the minimal correct implementation. Run the test and the required regression; restore all defined coverage metrics to 100%.
3. **REFACTOR:** with tests green, remove duplication, improve names and boundaries, extract the necessary reusable code. Rerun the tests, the quality analysis and the coverage check.
4. Repeat in small steps. A test written after a finished new implementation is not evidence that TDD was followed. A bug fix starts with a test that reproduces the bug.

For pure refactoring, the existing behavior tests are run first; missing guarantees are added and verified before the structural change. There is no need to deliberately break correct behavior for the sake of a red test. For a change to a document or declarative configuration, a check of the required new state — links, schema, contract or reproducible build — is defined and run first, then the change is made and the check is rerun. Every action has a verifiable expected result; we do not create meaningless tests merely for the fact that a line of code exists.

During RED, a failing new test is expected. **Every completed green step, integration, implementation commit, `done` and working build must have passing checks and 100% coverage.** RED must not be presented as a finished result.

## 3. What exactly 100% coverage means

The target is strict: **100% of all own executable code**, not only changed lines or files that tests happened to import.

| Part | Mandatory metric and evidence |
|---|---|
| Own Go code | 100% of executable statements in every own file/package and overall. The check reads the raw counters, not the rounded `100.0%` label. Own `main`, platform adapters and auxiliary tools are included in the inventory. |
| Own TypeScript/JavaScript, including React | 100% of lines, statements, functions and branches in every file and overall; the entire inventory of own executable sources must be explicitly included in the measurement, including files that are not imported. |
| SQL/migrations, CSS, declarative configuration and documents | Verified by the corresponding integration, component, visual, schema or structural scenarios. An executable-lines metric is not artificially attributed to them. The Go/TS logic that applies them is fully covered. |

The standard Go report measures statements; we do not call it measured branch coverage. Condition variants, errors and concurrency are additionally verified by behavioral tests. Profiles from unit and instrumented integration runs may be merged only for identical sources and settings. [Official Go measurement](https://go.dev/doc/build-cover).

For the frontend, Vitest and `@vitest/coverage-v8` are pinned (task 004, [quality-gate](../decisions/quality-gate.md)). The configuration explicitly includes the sources and enforces the four metrics for every file and overall; in addition, `go tool devtool check` derives the inventory from the repository listing and checks every file by raw counts, so the tool's settings alone are never the evidence of full coverage. [Official Vitest configuration](https://vitest.dev/config/coverage.html).

### Fixed measurement boundaries

- Startup, error handling, files without tests, scripts, probes and complex system adapters are never dropped from the inventory of own executable code. New own code in another language requires a measurement tool before it is accepted.
- External dependencies, the standard library, the tests themselves and unmodified reproducible generated code are accounted for separately. Each category has an explicit provenance rule; the name `generated` by itself does not justify an exclusion. Own wrappers are always measured.
- It is forbidden to lower thresholds, narrow the list of sources, add ignore directives for own logic, disable failing tests or replace complex behavior with a stub in order to reach 100%.
- Missing, empty, stale or incompatible profiles are a failed check. If there is no own executable code yet, the state is `not_applicable`, not a fabricated 100%. The first own code must already follow the rules.
- An unavailable environment yields `not_run`/`blocked`, not success or an exception to the threshold. 100% is the mandatory minimum for acceptance; it does not replace verifying the bytes of the real engine, SQLite or macOS.

## 4. Clean and reusable code

- Before a new implementation, check the existing components, functions, hooks and services. Shared behavior has a single owner; two copies of the same policy disguised as different screens are forbidden.
- Extract reusable modules according to real responsibility and verified use cases. Do not create general-purpose frameworks in advance, a global `utils` with mixed logic or a universal service for every case.
- **Go:** small domain packages, narrow interfaces at the point of use, explicit dependencies, `context`/cancellation and termination of goroutines, checked errors, separated pure policy. Formatting, `go vet` and the agreed analyzers are part of the check. [Effective Go](https://go.dev/doc/effective_go).
- **React/TypeScript:** strict mode, explicit DTOs, composition of small components and hooks, a single point of state ownership, managed subscriptions. Do not duplicate business state or Go policies in components; do not bypass types through unjustified `any` or assertions. [Thinking in React](https://react.dev/learn/thinking-in-react).
- **Wails:** the own bridge isolates the generated bindings; Go re-validates requests. **SQLite:** queries and migrations belong to the repository, transactions are explicit. **anacrolix:** direct access is isolated by the adapter; its types do not spread across the UI.
- Standards are verified against the official materials for the pinned versions. A deviation requires a reason and an architecture decision; the phrase "best practices" without concrete rules and checks is not a quality criterion.

## 5. No accumulation of technical debt

Refactoring is part of every TDD cycle and of the current task. In the affected scenario, the agent removes duplication, wrong dependencies, hidden effects and temporary workarounds before the work is finished. Refactoring is done in small steps with green tests and 100% coverage; required architecture changes are reflected in the documents immediately.

It is forbidden to accept `done` with "we will cover it later", a skipped error, a disabled test, a copy of a business rule, a temporary stub instead of the required implementation, an unexplained analyzer suppression or a TODO that postpones required quality. If a required fix cannot be completed, the task remains `blocked` with a concrete reason and a remediation plan; readiness is not declared.

An agreed future feature is not technical debt in itself. A test double for an external boundary is also acceptable if it is explicitly separated from the real verification. Do not mask an unfinished implementation with a test adapter and do not rewrite unrelated modules that have no connection to the current task. Previously discovered debt in the affected path is handled before that path is accepted, not automatically deferred to an undefined "later".

## 6. Feature documentation in the same change

The `docs/` folder is part of the project. For every feature, `docs/features/<feature>.md` is created and incrementally maintained according to the [template](../features/_TEMPLATE.md). Before the code, the behavior, boundaries and tests are described; after the code — the actual modules, contracts, handled failures, commands, results and state.

Do not mark a planned feature as implemented. A change to behavior, tests, schema, API or commands updates the corresponding documentation and the index at the same time. Architecture decisions are stored in `docs/decisions/`, evidence in `docs/verification/`; one feature may be developed across several tasks without copies of contradictory descriptions.

## 7. Blocking completion check

Before integration and the `done` status, the following must pass: behavior tests and the required regression, 100% of the defined metrics, completeness of the file inventory, formatting/types/analyzers, generated contracts, documentation check, review of the architecture change and of the absence of new technical debt. `go tool devtool check` ([quality gate](../features/quality-gate.md), task 004) automates this locally; a cloud service is not required. Only its `GATE PASSED` accepts a change; `check fast` and `check offline` report an incomplete gate.

Prototypes also work through TDD and 100% coverage and pass the same gate. A new kind of own code (another language, a new source directory) gets an inventory rule in the gate before it is accepted.

The work summary records: the session architecture plan, the tests and the expected RED reason, the actual GREEN commands, the refactoring performed, the raw coverage report and the source inventory for the same code, the feature documentation and the conclusion about debt. Checks that were not run do not count. A percentage is not reported as measured unless the collection tool was actually run.
