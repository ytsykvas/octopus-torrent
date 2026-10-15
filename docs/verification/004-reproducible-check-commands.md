# Report 004: the single local check command

**Task:** [004](../../product/tasks/004-reproducible-check-commands.md)  
**Date:** started 15 October 2026  
**Change version:** in progress (parent `32392e6`)  
**ADR:** [agent-tooling](../decisions/agent-tooling.md), [toolchain-versions](../decisions/toolchain-versions.md); `quality-gate` (O3) is created by this task

## Session architecture plan

Approved by the owner on 15 October 2026.

- **Goal:** replace the seven-step list in `.claude/commands/check.md` with one blocking command,
  `go tool devtool check`, implemented test-first in `cmd/devtool` + `internal/devtool`. It checks
  coverage independently of tool thresholds (full source inventory, raw counters), dependency age
  and vulnerabilities, a TS/React linter and the task plan. The open `release-guard` gap
  (substitutions in unquoted here-document bodies) is closed in the same task.
- **Owner's decisions:** linter = Oxlint 1.86.0 + oxlint-tsgolint 7.0.2003 (ESLint is impossible:
  every typescript-eslint release, including 8.71.2, needs `typescript <6.1.0`, the project uses
  7.0.2); network checks are part of the full `check`, and `check offline` marks them `not_run`.
- **Boundaries:** developer tooling only; the product layers and the Wails contract do not change.
  New CLI contract: `check` (full gate, the only one valid for `done`), `check offline`,
  `check fast` (no `.app` run, no network; both print `GATE INCOMPLETE`), `check deps`,
  `check-plan`, `plan-nav`. One line per step, `pass|FAIL|not_run`, never stopping early. A missing
  environment is a blocking `not_run`; a tier that does not exist yet (real-engine integration
  007/018, test-bridge UI tests 035) is a non-blocking `not_run`, always printed. A quick check of one
  changed module is documented as the tools' own commands, not a devtool mode.
- **Gate steps:** `npm ci`; npm effective cooldown config; typecheck; `oxlint --type-aware
  --deny-warnings`; Vitest coverage + an independent TS inventory check (raw counts, all four
  metrics, no ignore directives); frontend build; `gofmt`; `go vet`; bindings regenerated and
  compared by content; `go test -race -covermode=atomic` with raw counters; an instrumented `.app`
  launched and quit through an Apple Event; Go profile compatibility, merge and inventory; release
  build; `check-docs`; `check-plan`; dependency age (npm lockfile, `go list -m all`), `npm audit`,
  pinned `govulncheck`, a report of updates out of quarantine; a source hash before and after.
- **Reuse:** `System` injection and `fakeSystem` (extended with per-command outputs, HTTP, clock),
  `repositoryFiles`, `CheckLinks`/`anchors` from `docs.go`, `minReleaseAgeDays`, the instrumented
  binary technique of `cmd/devtool/main_test.go`, the `.app` coverage procedure from 003.
- **Stages, each green and committed separately:** A — release-guard gap; B — `check-plan` +
  `plan-nav`; C0 — lint spike (if tsgolint fails on TS 7.0.2, stop and ask the owner); C — runner,
  modes, coverage inventories, `.app` run, bindings, lint, source hash; D — `check deps` +
  govulncheck; E — documents, clean-copy run, this report, status `done`.
- **Documents:** ADR `quality-gate.md` (O3), feature doc `quality-gate.md`, index, agent-tooling and
  toolchain-versions ADRs, README commands, `CLAUDE.md`, `/check`, `fix-checks.md`,
  `dependencies.md`.

## Hook disabled locally during this task

On 15 October 2026 the owner chose to disable hooks for this task instead of approving every change
to `internal/devtool` and `cmd/devtool` in the `default` mode. This is the gitignored
`.claude/settings.local.json` with `"disableAllHooks": true`; the committed `.claude/settings.json`
is unchanged. While it is in place, the 14-day cooldown is followed by hand (release dates checked
before every dependency change). The file is deleted when the task is finished.

## Stage A — release-guard here-document gap

| Step | Test | Command | Result |
|---|---|---|---|
| RED | 4 deny cases: `cat <<EOF > notes.txt` with `$(npm i -g x)`; `cat <<-EOF` with `` `npx cowsay` ``; `git commit -F - <<EOF` with `'$(npx cowsay)'` (quotes are literal in the body); a quoted and an unquoted here-document in one command with the substitution in the unquoted one. 4 allow cases: `<<'EOF'` and `<<"EOF"` with substitutions, escaped `\$(…)` and `` \` ``, the substitution in the quoted one of two | `go test ./internal/devtool -run 'TestDecide(Allows\|Denies)CommandsThat'` | `fail` as expected: the 4 deny cases were allowed; the allow cases passed |
| GREEN | `stripHeredocBodies` keeps whether each delimiter was quoted; `decideCommand` decides the substitutions of every unquoted body (`bodySubstitutions`: quotes literal, only a backslash escapes) and the bodies themselves unless all go to data sinks; `substitutions` and `bodySubstitutions` share `expansions` | `go test ./internal/devtool` | `pass` |
| Coverage | raw counters, unit run | `go test -count=1 -cover -coverpkg=./... ./... -args -test.gocoverdir=…`; `covdata textfmt`; `awk 'NR>1 && $NF==0'` | `pass`: only `main.go:26` (`main()`, covered by the `.app` run) |

The ADR [agent-tooling](../decisions/agent-tooling.md) no longer lists the gap; its parsing section
describes the rule.

## Stage B — plan check and navigation

New code: `internal/devtool/plan.go` (pure `CheckPlan`, `RenderNavigation`, `PlanSummary` over a
path → content map), commands `check-plan` and `plan-nav` in `devtool.go` (`planFiles`,
`listedPath` shared with `check-docs`), `sortedKeys` made generic.

| Step | Test | Command | Result |
|---|---|---|---|
| RED | `plan_test.go`: a consistent 3-task fixture passes; 30 single mutations each produce their problem (broken link in a task and in the README, cycle, unknown or mislabelled dependency, unparsed dependency line, unknown, missing or uncovered requirement, duplicate requirement, missing or unknown matrix row, primary task not listing its ID or missing, missing matrix / scenario sections, scenario count and numbering, stale navigation, missing markers, unknown status, `in_progress` with a `planned` dependency, missing / repeated / malformed header lines, wrong title, duplicate task ID, empty requirements); navigation regeneration keeps everything outside the markers and is idempotent | `go test ./internal/devtool -run Plan` | `fail` as expected: `undefined: CheckPlan`, `PlanSummary`, `RenderNavigation` (the API did not exist) |
| RED | `devtool_test.go`: `check-plan` pass line and `PLAN` lines with exit 1; `plan-nav` writes only a stale block, reports an up-to-date one, refuses an unreadable plan, reports a failed write; a failing file listing | `go test ./internal/devtool` | `fail` as expected: exit 64 (unknown command) |
| GREEN | both | `go test ./internal/devtool` | `pass` |
| Real plan | `cmd/devtool` test `TestThePlanOfThisRepositoryIsConsistent` runs the built binary on this repository | `go tool devtool check-plan`; `go tool devtool plan-nav` | `pass   plan: 68 tasks, 196 dependencies, 80 requirements, 19 scenarios; navigation up to date`; `plan-nav: navigation already up to date` (no diff) |
| Control | status of 005 set to `done` in the navigation table only | `go test ./cmd/devtool -run Plan` | `fail` with `PLAN product/tasks/README.md: navigation table is out of date…`; file restored |
| Coverage | raw counters, unit run | as in stage A | `pass`: only `main.go:26` |

Instructions now say to change the status in the task file and run `plan-nav` (AGENTS.md,
`/task`, planning.md, tech-lead agent and its Codex mirror, the plan README).

## Stage C0 — lint spike

| Step | Command | Result |
|---|---|---|
| Release dates | `npm view <pkg> time` for every lock entry added by `npm --prefix frontend install --save-dev --save-exact oxlint@1.86.0 oxlint-tsgolint@7.0.2003` | `pass`: 27 entries, the newest 2026-09-28 (limit 2026-10-01); `npm install-scripts ls`: no unreviewed scripts; `npm audit`: 0 vulnerabilities |
| Type-aware rules on TS 7.0.2 | a temporary `src/zzSpike.ts` with an unawaited promise; `oxlint --type-aware -D typescript/no-floating-promises` | `pass`: `error typescript(no-floating-promises)`, exit 1; the file was deleted |
| RED | `oxlint --type-aware --deny-warnings` with `frontend/.oxlintrc.json` (categories correctness and suspicious = error, perf = warn, React hooks rules) | `fail`, 25 findings: `react-in-jsx-scope` ×17, `require-mock-type-parameters` ×3, `no-unsafe-type-assertion` ×2, `consistent-return`, `consistent-function-scoping`, `no-unassigned-import` |
| GREEN | `npm --prefix frontend run lint`; `typecheck`; `test:coverage` | `pass`; 10 tests, 100% on all four metrics |

Resolution of the findings: `react-in-jsx-scope` is off (the project uses the automatic JSX runtime,
`jsx: react-jsx`); `typescript/consistent-return` is off and replaced by the compiler option
`noImplicitReturns`, which accepts an exhaustive `switch` that the rule reports; the side-effect
import of `@testing-library/jest-dom/vitest` in the test setup is allowed by name; the test code was
fixed (typed `vi.fn`, `Object.assign` / `Reflect.deleteProperty` instead of type assertions on
`window`, a captured resolver instead of a placeholder function).

## Stage C — the check command and coverage inventories

New code in `internal/devtool`: `check.go` (modes `check`, `check offline`, `check fast`; the step
runner; `CheckNpmCooldown`), `frontendcov.go` (`FrontendInventory`, `CheckFrontendCoverage`,
`EscapeHatches`), `gocov.go` (`GoModulePath`, `CheckGoCoverage`), `appcov.go` (the instrumented
`.app` run). `System` gained `Capture`, `RemoveAll`, `MkdirAll`, `ReadDir`, `Sleep`; `fakeSystem`
answers per command line, in order.

| Step | Test | Command | Result |
|---|---|---|---|
| RED | `frontendcov_test.go`: inventory from the listing; uncovered file, unimported or excluded file, 9999/10000 rounded to 100%, skipped lines, missing / empty / invalid report, missing metric, report keys that are not absolute; ignore directives, lint disables, `it.skip` / `describe.only` / `test.todo`, Go `t.Skip` | `go test ./internal/devtool` | `fail`: `undefined: FrontendInventory`, `CheckFrontendCoverage`, `EscapeHatches` |
| RED | `gocov_test.go`: unit + .app merge; only `main()` deferred without the .app; uncovered main, uncovered block, missing / empty unit or .app profile, .app profile without `main.go`, different blocks for one file, different coverage modes, a file with functions missing from the profile, an unparsable file, a malformed line | same | `fail`: `undefined: CheckGoCoverage`, `GoModulePath` |
| RED | `system_test.go`: `Capture` combines both streams, directories, `Sleep` | same | `fail`: `OSSystem has no field or method Capture` (and the others) |
| RED | `appcov_test.go`: launch and Apple Event quit; already running, failed build, no directory, failed `open`, never starts, Automation refused (-1743), never quits (then killed), no counters, unreadable directory | same | `fail`: `undefined: appCoverageRun` |
| RED | `check_test.go`: a healthy fake repository passes every step in order; `check fast` and `check offline`; a failure never stops later steps; a mandatory `not_run` fails the full gate; 18 single failures each fail their own step; outside the repository root; `CheckNpmCooldown` | same | `fail`: `undefined: CheckNpmCooldown` |
| GREEN | all of the above | `go test ./internal/devtool` | `pass` |
| First real run | the repository | `go tool devtool check offline` | `GATE FAILED`: the gate found 5 uncovered blocks in its own new code (report tail truncation, a failed re-listing in `bindings` and at the end, a removed binding, a file only in the .app profile); tests added |
| GREEN | the repository | `go tool devtool check offline` | `GATE PASSED` in 23 s: every step `pass`, the .app launched and quit, merged Go profile with no zero block, engine integration and test-bridge UI tests `not_run (not implemented yet)` |

Controls with `go tool devtool check fast` (each change reverted afterwards):

| Control | Result |
|---|---|
| `export const broken: number = "text"` in `App.tsx` | exit 1: `FAIL typecheck`, `frontend build`, `release build` |
| `func broken() int { return "text" }` in `internal/app/app.go` | exit 1: `FAIL go vet`, `bindings`, `go tests`, `go coverage`, `release build` |
| an untested `frontend/src/untested.ts` | exit 1: `FAIL frontend tests` (Vitest thresholds) |
| the same file excluded in `vite.config.ts` | exit 1: Vitest passes, the independent inventory reports `frontend/src/untested.ts: not in the coverage report` |
| an untested exported Go function | exit 1: `FAIL go coverage` |

## Stage log

| Stage | State |
|---|---|
| A — release-guard here-document gap | done |
| B — plan check | done |
| C0 — lint spike | done |
| C — check command and coverage inventories | done |
| D — dependency checks | not started |
| E — documents and acceptance | not started |
