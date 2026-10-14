# OctopusTorrent documentation

**Current state:** requirements, mockup and plan are prepared; the first minimal `.app` exists (task 003); product features are not implemented yet. Documentation evolves together with the code through TDD, without deferred write-ups after implementation.

## Mandatory entry points

- [Agent rules](../AGENTS.md).
- [Requirements](../product/PROJECT_REQUIREMENTS.md), [concept](../product/PRODUCT_CONCEPT.md), [architecture](../product/APPLICATION_ARCHITECTURE.md).
- [TDD, 100% coverage, architecture analysis and no technical debt](engineering/TDD_AND_QUALITY.md).
- [Task plan](../product/tasks/README.md).
- [Feature description template](features/_TEMPLATE.md), [ADR template](decisions/_ADR_TEMPLATE.md).
- [Root README](../README.md): current state, directory map, next task.

## Structure

| Location | Purpose |
|---|---|
| `engineering/` | Mandatory engineering rules and the actual quality commands once they are implemented. |
| `features/` | One current description of each feature: behavior, modules, contracts, tests, commands, state. |
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

## Feature index

Before implementing a feature, create its document from the template and add a link with its status here. Change the same document throughout all related tasks.

| Feature | Status | Tasks |
|---|---|---|
| [Desktop shell, `.app` build and the first React → Go connection](features/desktop-shell-build.md) | `implemented` (prototype) | [003](../product/tasks/003-wails-macos-build-spike.md) |

No product capability (import, transfer, library) is implemented yet.

Recommended first topics: `.torrent`/magnet import, library and restore, transfer policy, file management, deletion, UI/accessibility, settings, tags/collections, rules/watch folder. The final split is determined by the actual responsibilities of the modules.

## Updates

Every change simultaneously updates the feature document, the index, the affected requirements/contracts and the links to checks. Before code, the planned behavior and test are recorded; after GREEN, the actual implementation. The statuses `planned`, `in_progress`, `blocked`, `implemented` do not replace evidence. For documentation, links, mandatory sections and correspondence to the real state are checked; absence of code is not presented as 100% coverage.

The existence of the interactive HTML mockup is not measured coverage of the application or of the mockup itself. Code that is later moved, changed or reused gets tests under the current policy; outdated development history does not count as TDD evidence.
