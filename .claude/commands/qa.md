Manual QA of the real .app for what this session built: brief the `macos-qa` agent, run it, and report expected vs observed.

`$ARGUMENTS` — optional focus ("only the runtime check", "offline").

## Step 1: What to test (read-only)

From the conversation and `git status --short` / `git diff --stat`: what changed, the requirement IDs
and acceptance criteria it serves (task file in `product/tasks/`), and anything declared out of scope.

## Step 2: Scenarios

Write numbered scenarios a user could perform in the window, each with the exact control titles, the
expected visible text, and the evidence to collect (text read, sockets, process). Include one negative
scenario when the feature has an error path. Scenarios that need the network off or a system setting
changed are marked for the owner — the agent reports them `BLOCKED`, it never changes settings.

## Step 3: Run

Launch **one** `macos-qa` agent with the scenarios and this context; it cannot see the conversation.
Never run two at once in one worktree (they would build and drive the same app). The agent targets
its own instance by PID, so other worktrees' dev apps are left alone. Do not read screenshots yourself.

## Step 4: Report

A table: scenario → expected → observed → PASS / FAIL / BLOCKED / NOT RUN, then the check that every
app instance the agent started has exited. Turn each FAIL into a concrete next step (a failing test to write first). Record the results in
the task's verification report when the scenarios are acceptance evidence.
