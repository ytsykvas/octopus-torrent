# 003 — Build the first Wails .app on the owner's Mac

**Status:** `in_progress`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** Gate/spike with a recorded result  
**Hard dependencies:** [002](002-repository-and-agent-entrypoint.md)  
**Requirements:** `REL-01`, `REL-02`, `STACK-05`, `UI-04`, `AI-07`, `QA-01`, `QA-02`, `QA-05`

## Outcome

Build a small real macOS application with Wails v2, React and TypeScript and verify that the local interface calls Go code in the finished `.app` package.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `REL-01`, `REL-02`, `STACK-05`, `UI-04`, `AI-07`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Choose compatible versions of Wails v2, Go, Node.js, React, Vite and TypeScript. Record the versions, the required macOS tools and the actual result of `wails doctor`; pin the chosen dependencies in the corresponding version files.
2. Create a minimal application from the React + TypeScript template, enable strict mode and use a single lockfile for frontend dependencies. Add one Go method with a simple JSON response and a call from the UI through Wails bindings.
3. Build a finished `.app` package that includes the local frontend assets. Do not use a CDN, remote fonts or a separate server to load the interface.
4. Stop the development server, open the package from Finder and perform the Go call. Repeat the launch without internet, quit the application and open it again.
5. Repeat the build from a clean copy of the repository. Record the exact commands, package architecture, signing state, system dependencies and the verified macOS version; separately state the limits of the claimed compatibility.

6. Before writing own startup/React code, prepare tests and a minimal coverage configuration per QA-01/QA-02. Verify the expected RED, then the first `.app` and 100% of own sources; an instrumented run checks the entry point. Do not wait for the future centralization in 004 to start testing; distinguish the generated scaffold from own wrappers.

## Artifacts and change locations

**Proposed locations:** `main.go; wails.json; go.mod; frontend/package.json; docs/verification/first-build.md`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/003-wails-macos-build-spike.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The built `.app` opens from Finder after the development server is stopped and shows the local UI without internet.
- [ ] A click in the real Wails window calls the Go method and displays the expected response.
- [ ] Launching the finished package does not invoke Go, Node.js or Wails CLI and does not require the user to install them.
- [ ] The build can be repeated from a clean copy of the pinned sources and dependencies using the recorded commands.
- [ ] The report contains the actual Mac configuration, system dependencies, signing state and the limits of the compatibility check; unverified macOS versions are not called supported.

## Checks

- Perform the first and the repeated builds; keep the exact commands, working directories, exit codes and package locations.
- After stopping the development server, check the launch from Finder, the Go method response, operation without internet and a repeated launch after quitting the program.
- Review the package contents and dynamic dependencies; verify that the UI does not load required resources from the network or the developer's directory.

**How to check:** use the commands chosen and actually executed in this task, and a check of the finished `.app`. Do not wait for the scripts of 004 to be created and do not substitute a frontend check in the browser for launching the package.

## Decisions, risks and boundaries

Use Wails v2 for this prototype; a migration to v3 is not part of the task. Verify API signatures by compiling against the chosen version. A successful launch on one Mac proves operation on that configuration, not compatibility with all macOS versions. The presence of Go/Node.js on the developer machine by itself does not prove that they are needed or not needed for the finished package: this is checked through the way the package is launched and its dependencies.

**Basis for technical decisions:** [Wails installation](https://wails.io/docs/gettingstarted/installation/), [Wails: how an application is structured](https://wails.io/docs/howdoesitwork/). Cross-check signatures and limitations against the pinned version; reading the API is not a passed integration check.


## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
