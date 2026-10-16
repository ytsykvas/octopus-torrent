# OctopusTorrent documentation

**Current state:** requirements, mockup and plan are prepared; the first minimal `.app` exists (task 003), one local quality gate checks every change (`go tool devtool check`, task 004) and the Go–TypeScript contract is fixed and executable in both languages (task 005); product features are not implemented yet. Documentation evolves together with the code through TDD, without deferred write-ups after implementation.

## Mandatory entry points

- [Agent rules](../AGENTS.md).
- [Requirements](../product/PROJECT_REQUIREMENTS.md), [concept](../product/PRODUCT_CONCEPT.md), [architecture](../product/APPLICATION_ARCHITECTURE.md).
- [TDD, 100% coverage, architecture analysis and no technical debt](engineering/TDD_AND_QUALITY.md).
- [Task plan](../product/tasks/README.md).
- [Feature description template](_FEATURE_TEMPLATE.md), [ADR template](decisions/_ADR_TEMPLATE.md).
- [Root README](../README.md): current state, directory map, next task.
- New here: [getting started](development/getting-started.md), then [workflow](development/workflow.md).

## Structure

| Location | Purpose |
|---|---|
| `engineering/` | Mandatory engineering rules; the command that enforces them is described in [the quality gate](tooling/quality-gate.md). |
| `app/` | The Go application: desktop shell, Wails facade, operation ledger; later services, engine, library, files. |
| `bridge/` | The Go–TypeScript contract and how React calls Go. |
| `frontend/` | React structure; later state, i18n, styles and screens. |
| `tooling/` | Developer tooling: quality gate, `devtool`, task plan, test torrents and the local swarm, agent tooling. |
| `development/` | Getting started, testing, the task-to-commit workflow. |
| `decisions/` | Significant decisions, alternatives and reasons following the [ADR template](decisions/_ADR_TEMPLATE.md); not a log of every cosmetic edit. |
| `verification/` | Evidence of TDD, checks, coverage and architecture conclusions; a specific task's verification report can be reused. |

## Decisions and evidence

| Document | Status |
|---|---|
| [Project baseline and open-decision register](decisions/project-baseline.md) | accepted, [001](../product/tasks/001-scope-and-decision-register.md); [report](verification/001-scope-and-decision-register.md) |
| Git repository and AI entry point | [002](../product/tasks/002-repository-and-agent-entrypoint.md); [report](verification/002-repository-and-agent-entrypoint.md) |
| [Tool and frontend dependency versions](decisions/toolchain-versions.md) (O2) | accepted, [003](../product/tasks/003-wails-macos-build-spike.md); [report](verification/003-wails-macos-build-spike.md) |
| [Target macOS and application identity](decisions/macos-target.md) (O1) | accepted, [003](../product/tasks/003-wails-macos-build-spike.md); review — 066 |
| [Repository language: English only](decisions/repository-language.md) | accepted, owner's decision of 15 October 2026 |
| [Agent tooling: Claude Code, Codex, octopus and Conductor](decisions/agent-tooling.md) | accepted, owner's decision of 15 October 2026; [report](verification/agent-tooling.md) |
| [Local quality gate](decisions/quality-gate.md) (O3) | accepted, [004](../product/tasks/004-reproducible-check-commands.md); [report](verification/004-reproducible-check-commands.md) |
| [Go–TypeScript contract: DTOs, numbers, errors, operations, ordering](decisions/api-contract.md) | accepted, [005](../product/tasks/005-domain-dto-and-bridge-contract.md); [report](verification/005-domain-dto-and-bridge-contract.md) |
| [Controlled test torrents and a closed local swarm](decisions/controlled-swarm.md) | accepted, [006](../product/tasks/006-controlled-torrent-fixtures.md); [report](verification/006-controlled-torrent-fixtures.md) |

## Documentation index

A feature document (from the [template](_FEATURE_TEMPLATE.md)) records one feature's behavior, design, tests and evidence and carries a status. A guide or reference explains how something works or how to do something. Before implementing a feature, create its document in its domain and add it here; change the same document throughout all related tasks.

### `app/` — the Go application

| Document | Kind | Status | Tasks |
|---|---|---|---|
| [Desktop shell, `.app` build, startup and quit](app/desktop-shell.md) | feature | `implemented` (prototype) | [003](../product/tasks/003-wails-macos-build-spike.md), [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |
| [Wails facade](app/facade.md) | reference | current | [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |
| [Operation ledger](app/operations.md) | reference | current | [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |

### `bridge/` — Go ↔ TypeScript

| Document | Kind | Status | Tasks |
|---|---|---|---|
| [Go–TypeScript bridge contract](bridge/contract.md) | feature | `implemented` (contract, ledger, pure rules) | [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |
| [Calling Go from React](bridge/calling-go.md) | guide | current | [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |
| [Snapshot, delta and operation ordering](bridge/ordering.md) | reference | current (used by 035) | [005](../product/tasks/005-domain-dto-and-bridge-contract.md) |
| [Adding a UI → Go command](bridge/adding-a-command.md) | guide | current | 024/025 first |

### `frontend/` — React

| Document | Kind | Status | Tasks |
|---|---|---|---|
| [Frontend structure](frontend/structure.md) | reference | current | [003](../product/tasks/003-wails-macos-build-spike.md) |

### `tooling/` — developer tooling

| Document | Kind | Status | Tasks |
|---|---|---|---|
| [Local quality gate](tooling/quality-gate.md) | feature | `implemented` | [004](../product/tasks/004-reproducible-check-commands.md) |
| [devtool command reference](tooling/devtool.md) | reference | current | [004](../product/tasks/004-reproducible-check-commands.md) |
| [Task plan](tooling/task-plan.md) | guide | current | [004](../product/tasks/004-reproducible-check-commands.md) |
| [Test torrents and the local swarm](tooling/test-torrents.md) | feature | `implemented` (test support) | [006](../product/tasks/006-controlled-torrent-fixtures.md) |
| [Agent tooling](tooling/agent-tooling.md) | feature | `implemented` | — |

### `development/` — working on the project

| Document | Kind | Status |
|---|---|---|
| [Getting started](development/getting-started.md) | guide | current |
| [Testing](development/testing.md) | guide | current |
| [Workflow: from a task to a commit](development/workflow.md) | guide | current |

No product capability (import, transfer, library) is implemented yet.

Recommended first topics: `.torrent`/magnet import, library and restore, transfer policy, file management, deletion, UI/accessibility, settings, tags/collections, rules/watch folder. The final split is determined by the actual responsibilities of the modules.

## Updates

Every change simultaneously updates the feature document, the index, the affected requirements/contracts and the links to checks. Before code, the planned behavior and test are recorded; after GREEN, the actual implementation. The statuses `planned`, `in_progress`, `blocked`, `implemented` do not replace evidence. For documentation, links, mandatory sections and correspondence to the real state are checked; absence of code is not presented as 100% coverage.

The existence of the interactive HTML mockup is not measured coverage of the application or of the mockup itself. Code that is later moved, changed or reused gets tests under the current policy; outdated development history does not count as TDD evidence.
