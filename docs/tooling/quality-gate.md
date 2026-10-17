# Local quality gate

**Status:** `implemented`  
**Requirements:** `AI-07`, `AI-08`, `QA-01`, `QA-02`, `QA-05`  
**Related tasks:** [004](../../product/tasks/004-reproducible-check-commands.md)  
**Last verified version:** task 004 commits; last full run in [report 005](../verification/005-domain-dto-and-bridge-contract.md)  
**Last updated:** 2026-10-15 (moved from `docs/features/quality-gate.md`)  
**See also:** [devtool command reference](devtool.md), [testing](../development/testing.md)

## Goal and behavior

A developer or an agent runs one command from the repository root and gets an unambiguous answer
whether the change may be accepted:

```sh
go tool devtool check            # full gate: the only one that accepts a change
go tool devtool check offline    # without the network steps → GATE INCOMPLETE at best
go tool devtool check fast       # also without the .app run → GATE INCOMPLETE at best
go tool devtool check deps       # the network steps alone
```

Every step prints `pass`, `FAIL` (with the tail of the tool's output) or `not_run`; no step stops
the others. The decision is in [quality-gate](../decisions/quality-gate.md).

## Architecture analysis before the change

Developer tooling only; the product layers and the Wails contract are untouched. The gate reuses the
`System` boundary of `internal/devtool` (every process, file, directory and HTTP call goes through
it, so the whole gate is unit-tested with `fakeSystem`), the repository listing and link checks of
`check-docs`, the plan check, and `minReleaseAgeDays`.

## Contracts and data

- Input: the working tree at the repository root (`git rev-parse --show-prefix` must be empty).
- Output: one line per step, `<status>  <step>  <detail>`, failure output indented; the last line
  is `GATE PASSED`, `GATE FAILED[: not run: …]` or `GATE INCOMPLETE: not run: …`
  (`DEPENDENCIES …` for `check deps`).
- Exit code: 0 for `PASSED`, and for `INCOMPLETE` in a partial mode; 1 otherwise; 64 for an
  unknown command.
- Files written: `frontend/coverage/`, `build/coverage/{go-unit,go-app}` and their `.txt` profiles,
  `frontend/dist`, `cmd/engine-spike/assets`, `cmd/engine-spike/build/bin/EngineSpike.app`
(instrumented), `build/bin/OctopusTorrent.app` (rebuilt as a normal release at the end). All are
  gitignored.

## Scenarios and TDD

The steps, in order: `npm ci` (`--offline` in the partial modes), npm cooldown, go env, typecheck, lint,
frontend tests + independent TS inventory, frontend build, gofmt, go vet, bindings, go tests with
raw counters, `.app` coverage run, Go coverage, escape hatches, scripts, product isolation, release
build, docs, plan, release age, npm audit, govulncheck, updates, the not-yet-built tiers, single code
version.

Since 007 the repository has more than one Wails app. Each one is a row of `wailsApps` in
`internal/devtool/appcov.go`: the product, and the temporary [engine spike](engine-spike.md).
- **bindings:** regenerates each app's bindings in its own directory (`env -C <dir> go tool wails
  generate module`) and compares them.
- **`.app` coverage run:** builds each app with `-coverpkg=octopustorrent/...`, launches it with
  `GOCOVERDIR`, finds its pid with a `pgrep -f` pattern anchored at the start of the bundle's
  executable path, quits it by its bundle ID, and accepts only counters that carry that pid.
- **go coverage:** requires every app's main file in the `.app` profile.
- **product isolation:** `go list -deps` of the product, with the release build's tags
  (`desktop,wv2runtime.download,production`), must not contain the spike, a `testutil` package or
  anacrolix. The anacrolix clause stays until 018. The RED cases
for each are listed in [report 004](../verification/004-reproducible-check-commands.md).

## Implementation and reuse

| File | Responsibility |
|---|---|
| `internal/devtool/check.go` | modes, the step runner, the report, the steps that wrap tools |
| `internal/devtool/toolchain.go` | the npm cooldown and go env steps |
| `internal/devtool/frontendcov.go` | TS inventory, raw-count check of the Vitest summary |
| `internal/devtool/escapehatches.go` | the escape-hatch scan of TS and Go sources and tests |
| `internal/devtool/gocov.go` | Go inventory (`go/parser`; Go under `testdata/` is a problem), profile parsing, compatibility, merge, `go.mod` module and tools |
| `internal/devtool/scripts.go` | the `scripts` step: a `*.sh` file may only build `devtool` and `exec` it |
| `internal/devtool/appcov.go` | the `wailsApps` table; instrumented build, launch, Apple Event quit and counters of every app |
| `internal/devtool/isolation.go` | the `product isolation` step: what the product binary must not link |
| `internal/devtool/deps.go` | lockfile and build-list parsing, registry dates and integrity, audits, the updates report |
| `internal/devtool/plan.go` | the task plan check used by the `plan` step, and the `check-plan` and `plan-nav` commands |
| `internal/devtool/docs.go` | the link and pointer checks used by the `docs` step, and the `check-docs` command |
| `internal/devtool/workspace.go`, `codexagents.go` | the workspace setup and run commands; the Codex agent mirrors |

## Checks and 100% coverage

The gate checks itself: `internal/devtool` is part of the Go inventory, so its own uncovered blocks
fail it (the first real run did exactly that). Package coverage 100.0% of statements; merged unit +
`.app` profile without a zero block. Real boundaries checked for real: `OSSystem` methods
(`system_test.go`, including `Get` against `httptest`), the `.app` run on the owner's Mac, the
registries over the network. Not built yet and printed as `not_run`: real-engine integration (018),
test-bridge UI tests (035). The engine contract tests of 007 are ordinary Go tests. The loopback swarm tests of 006 are ordinary Go tests and run in
the `go tests` step of every mode ([test torrents](test-torrents.md)).

## Refactoring and debt check

`check-docs` and `check-plan` share `listedPath` and `repositoryFiles` (`git ls-files -z`, so every
name is read as it is); their checks return a summary or problems (`docsCheck`, `planCheck`), which
the CLI prints and the gate reports through one `own` wrapper. The bindings and single-code-version
steps share `changedNames`. `sortedKeys` became generic. No ignore directives, no thresholds lowered,
no TODOs.

## Usage and recovery

- Prerequisites: Go 1.27.2, Node 26 / npm 11.20, Xcode Command Line Tools; a logged-in GUI session;
  the terminal's Automation permission for OctopusTorrent and for EngineSpike (macOS asks once per
  app; if refused, the `.app` step is `not_run` and names System Settings → Privacy & Security →
  Automation); the network for the full gate.
- `.app coverage run` `not_run: … is already running` — quit the named app (OctopusTorrent or
  EngineSpike, perhaps from another workspace) and run again.
- Ctrl-C during the `.app` step can leave an instrumented app running (it is started by `open`,
  outside the gate's process group) and the instrumented build in its `build/bin`: quit it
  (`osascript -e 'tell application id "dev.tsykvas.octopustorrent" to quit'`, or
  `dev.tsykvas.octopustorrent.enginespike` for EngineSpike) and run the gate again, which rebuilds
  the release `.app` at the end.
- A tool that hangs (a stalled `npm ci`, the first-time Automation prompt left unanswered) hangs the
  gate: steps have no time limit yet (see the ADR); answer the prompt or interrupt and rerun.
- `single code version` `FAIL` — something edited the files during the run (an editor, another
  agent); the report lists them; run again.
- `updates` lists versions out of quarantine — take them in the next task that changes dependencies
  ([dependencies.md](../../.claude/docs/dependencies.md)).

## Change history and evidence

- 2026-10-15, task 004: the gate, the plan check, Oxlint, the dependency checks —
  [report 004](../verification/004-reproducible-check-commands.md).
