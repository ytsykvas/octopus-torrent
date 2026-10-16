# Task plan

> Last updated: 2026-10-15

The work is split into 68 numbered tasks in `product/tasks/`. Each file is the single source of its
title, status, phase and hard dependencies. `product/tasks/README.md` holds the plan's rules, the
requirement and scenario matrices, and a navigation table generated from the task files. Two
devtool commands keep it consistent.

## A task file

- **Header:** status, phase, priority, size, hard dependencies (links), requirement IDs.
- **Body:** outcome, "Read before starting", work order, artifacts, acceptance criteria (checkboxes),
  checks, decisions, risks and boundaries, the mandatory TDD section, the handoff rules.
- **"From NNN" notes:** an earlier task records what a later one must do, for example the
  [api-contract](../decisions/api-contract.md) notes in 013, 024 and 028.
- **"Execution result":** added at the end when the task finishes: date, status, changes, checks,
  limitations, next task.

## Statuses

| Status | Meaning |
|---|---|
| `planned` | not started; the steps are a plan |
| `in_progress` | being executed |
| `blocked` | a named obstacle with a way to unblock it |
| `done` | criteria and checks met, evidence saved, documents and statuses consistent; no unchecked acceptance box |

A task may start when every hard dependency is `done`. Check results (`pass`, `fail`, `not_run`)
are not task statuses.

## Changing a status

1. Edit `**Status:**` in the task file.
2. Run `go tool devtool plan-nav`. It rewrites only the `TASKS-NAV` block of `product/tasks/README.md`.
   Never edit that block by hand.
3. Run `go tool devtool check-plan`.

## What `check-plan` verifies

- Every task file has a valid header, and IDs are unique.
- Every link resolves.
- The dependency graph has no cycle.
- No `done` or `in_progress` task has an unfinished dependency, and a `done` task has no
  unchecked `- [ ]` item anywhere in its file.
- Every requirement ID exists, and all 80 are covered with their primary tasks.
- The scenario table is complete, and the navigation block is up to date.

Each problem prints as `PLAN <problem>`. The full gate runs the same check as its `plan` step.

## Evidence and handoff

- **Report:** `docs/verification/<NNN>-<slug>.md` holds the session plan, RED/GREEN per stage,
  results, refactoring, coverage and debt.
- **Execution result:** the task file's final section.
- **Repository state:** README "Current state" and the next tasks.

Context needed by the next agent is written into the repository, never left in the chat.
