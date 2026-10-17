# Planning a numbered task

Every numbered task starts in plan mode (`/task`). The plan is what the owner approves and what the
session report records, so it must leave no design decision open: could another agent implement it
from the plan alone, without making a single design choice?

Plans are written in English, like everything in the repository.

## What the plan must decide

| Area | The plan contains |
|---|---|
| Context | The task, its requirement IDs, the open decisions it owns, and what changes for the owner. |
| Boundaries | Which layers change (React, bridge, facade, service, engine adapter, repository, files/platform) and which contracts (DTOs, events, bindings, schema). If the architecture does not change, why. |
| Reuse and refactoring | Existing code to reuse; refactoring needed in the touched scenario. |
| Files | Every file with `new` / `edit` / `delete`; for an edit, the function or section it changes. |
| Tests first | Per step: the test file written **before** the code, and the exact failure its RED run must show (a missing behavior, not a broken environment). |
| Coverage | How 100% is proven: Go raw counters (and whether the real `.app` run is needed), TypeScript per-file metrics. |
| Real boundaries | What is verified for real (disk, SQLite, the engine, macOS) and what only with a test double, named as such. |
| Decisions | Choices the owner must make, asked before the plan is final — never left as "TBD". ADRs to write. |
| Dependencies | Any new or bumped package with its version and release date (≥ 14 days, [dependencies.md](dependencies.md)). |
| Documents | Feature doc, `docs/README.md` index, ADRs, verification report, task status in both places, README. |
| Out of scope | What is deliberately not touched. |
| Verification | The commands that prove it (normally [`/check`](../commands/check.md) plus `.app` scenarios via `/qa`), and which checks may legitimately remain `not_run`. |

## Self-assessment before approval

The owner expects evidence that the plan was challenged before it is shown, not a confident summary.
Before asking for approval:

1. Re-read the plan adversarially, as a reviewer looking for what will break. Check assumptions in
   the source of the pinned libraries, not only their documentation. Look in particular at:
   - lifecycles and shutdown;
   - errors crossing a boundary;
   - validation of input;
   - restart and recovery;
   - timing and concurrency;
   - staging;
   - each acceptance criterion with no test mapped to it.
2. Fix every gap you find in the plan itself.
3. End the plan with a **"Self-assessment: N/10"** section:
   - If the rating changed during the review, give both numbers.
   - List what the review fixed.
   - Name what remains weak and why it is acceptable.

The rating has no minimum. It exists to show the real risks, so a low number with a precise list of
what remains weak is worth more than a high one: never raise it to get approval. Close the gaps you
can close before asking, for example:
- verify an assumption in the pinned source;
- turn a manual check into a gate check;
- replace a timing-based assertion with a deterministic one.

If the owner answers the approval request with "Оціни план від 1 до 10" ("rate the plan from 1 to
10"), the self-assessment was missing or not convincing. Review the plan again, change the rating
only for gaps that were actually fixed, and ask for approval again.

## What does not belong

Research narrative, alternatives already rejected (state the decision; the ADR keeps the reasoning),
pre-written code beyond an anchor for an edit, project rules that already live in `AGENTS.md`, and
placeholders.

## After approval

The approved plan goes into the "session plan" of `docs/verification/<NNN>-<slug>.md`; the status
becomes `in_progress` in the task file, and `go tool devtool plan-nav` regenerates the navigation table.
