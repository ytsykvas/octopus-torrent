# OctopusTorrent: mandatory agent rules

A personal local macOS application project: React + TypeScript strict/Vite, Wails v2, Go, anacrolix/torrent, SQLite. Source of behavior: [requirements](product/PROJECT_REQUIREMENTS.md); work order: [tasks/README.md](product/tasks/README.md). Do not change the agreed stack or product scope on your own.

## At the start of EVERY session

1. Read the [architecture](product/APPLICATION_ARCHITECTURE.md), the [engineering rules](docs/engineering/TDD_AND_QUALITY.md), the current task, the feature documentation and the actual code changes.
2. Analyze boundaries, state, dependencies, side effects, errors and resource cleanup. Find code to reuse.
3. Before editing, write down a short architecture plan, tests, the required refactoring and documentation. If the design does not need to change, explain why. Repeat this when continuing work in a new session.

## For every change

- TDD: **RED → GREEN → REFACTOR**. Prove new behavior/fixes with a failing test first; refactoring starts from behavior guarantees and keeps the tests green.
- **100% coverage of all owned executable code** by the exact metrics and the fixed inventory in the policy. Prototypes, startup, scripts and owned system adapters are included too. Do not lower thresholds, hide files or disable tests.
- Clean and reusable code: explicit dependencies, small modules, a single implementation of shared policy, React composition, Go services/adapters, SQL only through the repository. The official rules of the pinned versions have concrete checks.
- Refactor the affected scenario immediately on green tests. Do not leave new technical debt, duplicates, required stubs or "we will fix it later". If the cleanup is not finished, the task is `blocked`, not `done`.
- Create and update the [feature documentation](docs/features/_TEMPLATE.md) and the [index](docs/README.md) in the same change; significant decisions go to `docs/decisions/`.
- Completion is allowed only after tests, 100% coverage, analyzers, up-to-date documents and a check that there is no debt. In the report, keep the architecture conclusion, the RED/GREEN commands, the refactoring and the actual results.

The application itself is currently planned; an HTML mockup exists. Do not invent executed tests or a current percentage for any artifact. An unavailable check is `not_run`/`blocked`. During RED, the expected failure is not a finished change. Synchronized `sources/`, if present, stay read-only. Do not overwrite other people's changes or user files.

**Language:** everything written into the repository — documents, task files, reports, agent instructions, code comments, commit messages — is English; reply to the owner in Ukrainian ([decision](docs/decisions/repository-language.md)).

## Module boundaries

Dependency direction: React → own bridge → Wails facade (`AppAPI`) → Go services → engine adapter / repository / file service. Details: [architecture §2–§4](product/APPLICATION_ARCHITECTURE.md#2-components-and-dependency-directions); paths: in the [README](README.md#directory-map) (most do not exist yet).

| Layer | May | May not |
|---|---|---|
| React (`frontend/src/features`, `state`) | Show DTOs/snapshots, send commands through the bridge, localize texts. | Access the file system or the engine; duplicate Go policy (pause, upload, wanted); decide whether a data write succeeded. |
| Frontend bridge (`frontend/src/bridge`) | The single point for calling generated bindings and subscribing to events; a test bridge for browser tests. | Contain business rules. |
| Wails facade (`internal/app`) | Accept a DTO, re-validate it, delegate to a service, return a result/operationID and `AppError`. | Pass engine objects, pointers or channels to TS; run shell commands at the UI's request. |
| Go services | Policy, command ordering for a single torrent, import, recovery. | Write SQL directly or touch anacrolix outside the adapter. |
| Engine adapter (`internal/torrentengine`) | The only place with anacrolix/torrent types; converts state and errors into own DTOs. | Delete files (`Drop()` is not deletion). |
| Repository (`internal/library`) | All SQL, migrations, explicit transactions. | Edit the engine's piece completion. |
| Files and platform (`internal/files`, `internal/platform`) | Dialogs, folder creation, validation of every path, Trash. | Delete without an explicit list and confirmation; trust a path from the UI without validation in Go. |

## File operations

- The user's payload goes only into the folder they chose; no action deletes files without a separate selection and confirmation (DEL-01–05).
- Go re-validates every physical path: save path boundaries, symlinks, collisions, shared files.
- Spikes and tests work only with their own synthetic files in temporary directories or `testdata/`; never with the owner's personal torrents or downloads.
- Personal `.torrent` files, downloaded data, working databases and logs are not committed ([.gitignore](.gitignore)).

## Dependencies

Policy: [toolchain-versions](docs/decisions/toolchain-versions.md). For npm and Go modules (including transitive ones), use the latest stable version **published at least 14 days ago**; pre-releases are forbidden; do not remove the 14-day cooldown. Go/Node patch releases are taken immediately, new minor/major releases after 14 days. Before adding or updating, check the release date, `npm audit`, `govulncheck`; npm install scripts only through an explicit `allowScripts`. Update the version table in the ADR in the same change.

## Generated code (Wails bindings)

- `frontend/wailsjs/` is generated by Wails from the facade's Go methods; **do not edit it by hand**. If the Go contract changed, regenerate the bindings with `go tool wails generate module` ([README](README.md#build-and-check-commands)) and commit them together with the change.
- React accesses bindings only through `frontend/src/bridge`; own wrappers are measured by coverage, unchanged generated code is accounted for separately according to the [policy](docs/engineering/TDD_AND_QUALITY.md#fixed-measurement-boundaries).

## Agent tooling

Shared by Claude Code, Codex, octopus and Conductor: review agents, commands, and one instruction file per action (review, pull request, commit message, fixing checks, addressing review, resolving conflicts) — index in [.claude/docs/README.md](.claude/docs/README.md), reasons in the [ADR](docs/decisions/agent-tooling.md). The `release-guard` hook (`go tool devtool hook release-guard`) blocks commands that would bypass the dependency cooldown; never work around it. Workspace setup/run logic is Go in `cmd/devtool` and `internal/devtool` and follows the same TDD and 100% rules as the product.

## How to find, execute and hand off a task

1. The next task is in the [README](README.md#current-state) and the [plan navigation](product/tasks/README.md#navigation-of-all-tasks): status `planned`, all dependencies `done`.
2. Criteria, steps and checks are in the task file; requirement IDs are in the [requirements](product/PROJECT_REQUIREMENTS.md); open questions are in the [decision register](docs/decisions/project-baseline.md#6-open-decision-register).
3. Change the status in the task file and in the navigation table at the same time.
4. Decisions: `docs/decisions/<slug>.md` following the [ADR template](docs/decisions/_ADR_TEMPLATE.md); evidence: `docs/verification/<NNN>-<task-slug>.md` (session plan, RED/GREEN commands and results `pass`/`fail`/`not_run`, refactoring, coverage, debt); features: `docs/features/<feature>.md`.
5. Handoff: at the end of the task file, an "Execution result" section with the date, status, changes, checks, limitations and the next task. The commit explains the result and how it was verified (AI-09). Context needed by the next agent is stored in the repository, not in the chat.
