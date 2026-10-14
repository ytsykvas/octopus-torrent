# 064 — Measure large libraries and long-running operation

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** mandatory feature or check of the agreed scope  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [062](062-automation-end-to-end-gate.md), [054](054-global-upload-ui-mvp-gate.md), [040](040-app-shell-fixed-geometry.md), [046](046-post-import-file-folder-tree.md), [027](027-metrics-status-snapshots.md)  
**Requirements:** `UI-16`, `UI-15`, `STACK-02`, `AI-08`

## Outcome

Measure the load on the owner's Mac and optimize specific problems.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `UI-16`, `UI-15`, `STACK-02`, `AI-08`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Choose the number/depth of synthetic entries and the response/memory/events budgets after the first measurement.
2. Check many entries and expanded trees without public downloads.
3. Profile Go/React/bridge/resources locally without a production debug server.
4. Run a bounded long-running cancel/start/drop/watch/offline/restart soak.
5. Optimize only a measured bottleneck; do not lose operation completion and do not break focus with virtualization.
6. Record before/after metrics, the target Mac and known limits.

## Artifacts and change locations

**Proposed locations:** `docs/verification/performance.md; targeted profiling`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/064-scale-profiling-lifecycle-soak.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] Window size and scrolling are stable.
- [ ] Listeners/goroutines do not grow after repeated cycles.
- [ ] The optimization has a measured result.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] An optimization that changes the DOM tree or focus has targeted accessibility rechecks from 065; an old report is not credited to a changed build.

## Checks

- Go through AS-17 with long names and a deep tree.
- Check bulk/search under updates, idle/active and resizing.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Choose quantitative budgets by measuring the owner's Mac. Do not introduce arbitrary SLAs, a million entries or a specific virtualization framework as hidden requirements.

**Basis for technical decisions:** [Go diagnostics](https://go.dev/doc/diagnostics), [React external store](https://react.dev/reference/react/useSyncExternalStore). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, write down the change plan and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and check evidence.
- Write a short result: what changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, how to unblock it and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave necessary context only in the chat.
