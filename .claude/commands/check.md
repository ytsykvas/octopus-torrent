Run every OctopusTorrent health check, report the results, and offer to fix what failed.

The gate is one command (task 004, [quality gate](../../docs/features/quality-gate.md)). Run it from
the repository root:

```bash
go tool devtool check
```

It runs every step in order and **never stops early**: npm ci, npm cooldown, go env, typecheck, lint,
frontend tests with the independent 100% inventory, frontend build, gofmt, go vet, bindings, go
tests with raw counters, the instrumented `.app` run, Go coverage, escape hatches, release build,
docs, plan, release age, npm audit, govulncheck, updates, and a check that no file changed during
the run. Each step prints `pass`, `FAIL` (with the tail of the tool's output) or `not_run`; the last
line is `GATE PASSED`, `GATE FAILED` or `GATE INCOMPLETE`. Only `GATE PASSED` from the full `check`
allows a commit.

The full gate opens an OctopusTorrent window for a few seconds and needs the network. If the owner is
offline, run `go tool devtool check offline` and say plainly that the network steps are `not_run` and
the gate is incomplete. Do not run `check fast` as `/check`: it is the TDD loop, not the gate.

Two lines are always `not_run (not implemented yet)` — real-engine integration and test-bridge UI
tests; they do not block and are not to be reported as failures.

## Report

Quote the step lines that are not `pass`, and the final line. For each `FAIL`: the step, the
`file:line` or command it names, and why. A mandatory `not_run` (network, Automation permission,
another OctopusTorrent running) names its fix; relay it. `updates` listing versions out of quarantine
is not a failure: mention them as the next dependency update.

If something is fixable, ask: "Found N issue(s). Fix them? 1. Yes 2. No" and wait. Fixing follows
[fix-checks.md](../docs/actions/fix-checks.md). If the gate passed, say so in one line.
