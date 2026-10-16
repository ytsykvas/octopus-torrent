# 004 — Record the build and local check commands

**Status:** `done`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [003](003-wails-macos-build-spike.md)  
**Requirements:** `AI-07`, `AI-08`, `QA-01`, `QA-02`, `QA-05`

## Outcome

Create and verify a single set of commands for development, build and local checks, so that subsequent tasks can refer to reproducible actions with an unambiguous result.

## Read before starting

**Permission mode (added 2026-10-15):** this task changes `cmd/devtool` / `internal/devtool`, the
`release-guard` hook's own code. In a permission mode that approves prompts by itself (`auto`,
`acceptEdits`, `bypassPermissions`) the hook blocks those edits; the owner switches Claude Code to the
`default` mode (Shift+Tab) before the work and approves each change ([ADR](../../docs/decisions/agent-tooling.md)).
Close the open `release-guard` gap recorded there (substitutions in unquoted here-document bodies)
test-first in the same session.

- [Task execution rules](README.md): conditions for starting and completing a task, statuses and evidence of checks.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-07`, `AI-08`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Outcomes and ADRs of all mandatory prerequisites above; without the required evidence, do not start the dependent part.

## Work order

1. Based on the successful build from 003, set up commands for running development, building the `.app`, TypeScript checking, frontend tests, Go tests, formatting, `go vet` and Wails bindings generation.
2. Create a shared local check script. It must run from a documented location, return a non-zero exit code on error and not continue as though a failed check had succeeded.
3. Separate fast checks from concurrency checks via the Go race detector, future real-engine tests and UI tests with the test bridge. If the corresponding tests do not exist yet, mark this explicitly; an empty run is not evidence that a feature works.
4. Verify installation from the pinned dependencies, bindings generation and the commands from a clean copy of the repository. Remove the scripts' dependence on personal absolute paths and hidden manual settings.
5. Update the README: state the command, working directory, prerequisites, purpose and verified result. Separately describe actions that require the internet, a real Mac or a manual check of the `.app`.

6. Add a local plan check: unique task and requirement IDs, existing links, an acyclic graph, consistent statuses and coverage. The status, title and dependencies in the task file are the source for the navigation table; regenerating it must change only the marked block of the README. Do not require every dependency to have a smaller number: after a future split this is checked by topological sorting.
7. Give commands separately for a quick check of a changed module, integration with the real engine and a full build. Do not run the whole long suite for every small change; choose checks by the specific risk.

8. Before implementing the check tool, add negative examples: an uncovered file, an unimported TS module, uncovered Go startup, a percentage below 100%, a missing/stale profile and an impermissible exclusion. Each must produce a non-zero exit code.
9. Implement the check of the full inventory and raw counters according to the engineering policy: Go and frontend are checked separately, per file and in total; merge only compatible profiles of the same sources. Do not call an empty set successful coverage.
10. **Added on 2026-10-15 by the owner's decision** (policy [toolchain-versions](../../docs/decisions/toolchain-versions.md)): a dependency check as part of the local command — the age of every locked npm and Go module version ≥ 14 days (error if younger), `npm audit` and `govulncheck` (error on a vulnerability), a `npm outdated` / `go list -m -u all` report on updates that have left quarantine. Mark network checks separately from offline checks. **Added on 2026-10-15 with the agent tooling** ([ADR](../../docs/decisions/agent-tooling.md)): implement the age check in `cmd/devtool` / `internal/devtool`, reusing `minReleaseAgeDays` (one owner of the policy), and include npm's effective cooldown settings (`npm --prefix frontend config get min-release-age before min-release-age-exclude`): it is the guarantee behind the `release-guard` hook, which is only a safety net. Fold the step list of `.claude/commands/check.md` and `go tool devtool check-docs` into the single command, and update `/check` to call it.
10. The shared local quality command blocks acceptance on failed tests, coverage below 100%, a type/analyzer error, non-reproducible bindings or outdated documentation. Fast runs help TDD but do not replace the full blocking run before integration. Keep RED/GREEN evidence for the check tool itself.

## Artifacts and change locations

**Proposed locations:** `cmd/devtool` + `internal/devtool` (existing Go tooling, see the [agent-tooling ADR](../../docs/decisions/agent-tooling.md)); `README.md; go.mod/go.sum; frontend lockfile`. This is a map of future code; after the initial setup, check the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/004-reproducible-check-commands.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [x] The documented commands run from a clean copy with the stated prerequisites, without hidden manual steps.
- [x] A control TypeScript error and a control Go error cause a failed result of the corresponding check and of the shared script.
- [x] Wails bindings can be regenerated without manual editing; the result contains no unexplained changes from the same sources.
- [x] The README distinguishes existing checks from future engine tests, test bridge tests and checks of the real `.app`.
- [x] Unavailable or not yet implemented checks are marked `not_run`, not as success.

- [x] The plan check detects a broken link, a cycle, an unknown ID, a mismatched status and lost coverage; on the current plan it finishes successfully.
- [x] The navigation is regenerated from the task state without overwriting manually maintained matrices, explanations and open decisions.

- [x] An uncovered own file and a metric below exactly 100% block the shared command; rounding, changing include/exclude or a missing report do not allow a bypass.
- [x] The full list of sources, all defined metrics, profile compatibility and a single code version are verified; the results are reproducible.
- [x] The initial commands from 003 are kept or moved without weakening TDD/100%; no successful tests of a feature are invented before the feature exists.

## Checks

- Run the full documented set of commands from a clean copy and record the exit codes.
- One at a time, temporarily add a safe TypeScript error and a Go error, make sure the check exits non-zero, then remove the errors and repeat a successful run. Do not leave these changes in the task result.
- Repeat bindings generation and compare the result; verify that the scripts work in a directory with a different absolute path.
- If an external security check cannot obtain the required data, record the reason and `not_run`; do not present local compilation as its replacement.

**How to check:** run the scripts created in this task and keep the actual commands and results. Future tests must not block creating the command set, but their absence must be visible.

## Decisions, risks and boundaries

Check commands must be simple and runnable by an AI agent without knowledge of the project history. Do not hide errors by ignoring exit codes or by a generic success message. This task creates the foundation for checks; it does not prove that the not yet implemented engine, UI or file operations work.

**Basis for technical decisions:** [Go security practices](https://go.dev/doc/security/best-practices). Reconcile signatures and constraints with the pinned version; reading the API is not a passed integration check.


## Mandatory TDD cycle and architecture review

At the start of every session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan for the change and the required refactoring. This also applies when continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for the failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all our own executable code** by the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architectural decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are run; the absence of executable code is not called 100%. In the task report, record the architectural conclusion, RED/GREEN, the refactoring done, the inventory/profile and the check that no debt remains. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and evidence of checks.
- Record a short outcome: what changed, exact checks with pass/fail/not_run, ADR, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.

### Execution result (15 October 2026)

- **Status:** `done`. Evidence: [report 004](../../docs/verification/004-reproducible-check-commands.md); feature: [quality-gate](../../docs/features/quality-gate.md); ADR: [quality-gate](../../docs/decisions/quality-gate.md) (O3 accepted), updated [toolchain-versions](../../docs/decisions/toolchain-versions.md) and [agent-tooling](../../docs/decisions/agent-tooling.md).
- **Created:** `go tool devtool check` (modes `offline`, `fast`, `deps`) in `internal/devtool` (`check.go`, `frontendcov.go`, `gocov.go`, `appcov.go`, `deps.go`); `go tool devtool check-plan` and `plan-nav` (`plan.go`); Oxlint with type-aware rules (`frontend/.oxlintrc.json`, `npm run lint`); `govulncheck` as a `go.mod` tool. The `release-guard` gap with unquoted here-documents is closed. After `/branch-review`: the `go env` step, lockfile `resolved`/`integrity` tied to the registry, wider escape-hatch and lint rules, a `check-plan` rule against unchecked items in `done` tasks, and the here-document forms found by the security review closed in `release-guard`. vite 8.3.2 and vitest / @vitest/coverage-v8 5.0.3 adopted (out of quarantine).
- **Checks:** RED → GREEN for every check function and every review fix; `internal/devtool` at 100%; with the hook active, on the repository `go tool devtool check` → `GATE PASSED` (21 s), `check offline` and `check fast` → `GATE INCOMPLETE`, exit 0; from a clean copy at another absolute path `setup.sh`, `check` (`GATE PASSED`) and `wails doctor` exit 0; controls (TS type error, Go compile error, untested TS file, the same file excluded in `vite.config.ts`, untested Go function) each exit 1; `check deps`: 186 npm versions and 147 Go modules at least 14 days old, no vulnerabilities.
- **Limitations:** the full gate needs a GUI session, the network and the terminal's Automation permission, one workspace at a time; the Go release age is the publisher-set commit time; steps have no time limit; the `.app` run must get an isolated data directory before the application has persistence (quality-gate ADR); real-engine integration (007, 018) and test-bridge UI tests (035) are printed as `not_run` until those tasks build them; verified only on Apple M4 / macOS 27.0.1.
- **Next task:** [005](005-domain-dto-and-bridge-contract.md); also unblocked: [006](006-controlled-torrent-fixtures.md), [008](008-sqlite-driver-durability-spike.md), [017](017-local-diagnostics-privacy.md) after 005, [035](035-react-bridge-state-resync.md) after 005.
