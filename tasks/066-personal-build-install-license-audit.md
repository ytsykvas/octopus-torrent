# 066 — Build and install the personal release

**Status:** `planned`  
**Phase:** P7 — Reliability, personal build and handoff  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [063](063-failure-race-fuzz-regression.md), [064](064-scale-profiling-lifecycle-soak.md), [065](065-native-accessibility-language-gate.md)  
**Requirements:** `REL-01`, `REL-02`, `STACK-05`, `DATA-02`

## Outcome

Build and install the verified personal application package.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `REL-01`, `REL-02`, `STACK-05`, `DATA-02`, the relevant criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTOs, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Build with the pinned tools and record the hash, version and architecture of the package.
2. Check the signature, linked libraries, native bundling and the actual installation.
3. Run without a dev server/Go/Node/Homebrew runtime.
4. Prepare a list of dependencies/licenses and an available vulnerability check.
5. Run the packaged Finder/magnet/drop/dialog/clipboard/quit/restart/offline smoke test.
6. Public distribution and Developer ID/notarization do not block the personal release; DMG only if needed.

## Artifacts and change locations

**Proposed locations:** `build .app; dependency inventory; docs/verification/personal-install.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** the record `docs/verification/066-personal-build-install-license-audit.md` or an exact link to a test or build report in the context handoff. One report may be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The owner installs and launches the same package.
- [ ] Assets are local; there are no analytics/cloud access checks.
- [ ] Evidence of the signature and dependencies is recorded.
- [ ] The required checks are performed at the proper boundary; a test that was not run or failed is not called successful.

- [ ] Reports 063/064/065 apply to the same code and configuration from which the package was built; changes after the checks have an explained targeted recheck.

## Checks

- Check cold/warm native inputs.
- Check Trash, settings persistence and launch without internet.

**How to run:** use the actual commands from [004](004-reproducible-check-commands.md) and the root README. Record the command, working directory, configuration, test data, exit code and observations. Checks of the real engine, the test UI and system capabilities in the built `.app` are kept separate.

## Decisions, risks and boundaries

Technical tracking of dependency licenses does not create a product licensing or sign-in system. Public distribution and notarization do not block the personal build.

**Basis for technical decisions:** [Wails build](https://wails.io/docs/gettingstarted/building/), [anacrolix LICENSE](https://github.com/anacrolix/torrent/blob/master/LICENSE), [Go security practices](https://go.dev/doc/security/best-practices). Check signatures and constraints against the pinned version; reading the API is not a passed integration check.

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
