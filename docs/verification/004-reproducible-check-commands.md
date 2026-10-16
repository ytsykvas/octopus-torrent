# Report 004: the single local check command

**Task:** [004](../../product/tasks/004-reproducible-check-commands.md)  
**Date:** 15 October 2026  
**Change version:** commits `af0ecdf` … the 004 stage E commit (parent `32392e6`)  
**Feature:** [quality-gate](../features/quality-gate.md); **ADR:** [quality-gate](../decisions/quality-gate.md) (O3), [agent-tooling](../decisions/agent-tooling.md), [toolchain-versions](../decisions/toolchain-versions.md)

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
is unchanged. While it was in place, the 14-day cooldown was followed by hand (release dates checked
before every dependency change; the security review re-checked every new npm and Go version). The
file was deleted at the end of stage E, before the final commit; the hook then refused a probe
(`true || npm i -g left-pad@1.3.0`, short-circuited so nothing could install) with "A global npm
install is blocked", and the final gate runs below were made with the hook active.

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

## Stage D — dependency checks

New code: `internal/devtool/deps.go` (`NpmLockPackages`, `GoModuleVersions`, `EscapeModulePath`,
`ReleaseAgeProblem`, the steps `release age`, `npm audit`, `govulncheck`, `updates`, bounded
concurrent downloads), `GoTools` in `gocov.go`, `System.Get` / `Now`, mode `check deps`. The
network steps belong to the full `check`; `check offline` and `check fast` report them `not_run`.

| Step | Test | Command | Result |
|---|---|---|---|
| Release dates before adding | `go get -tool golang.org/x/vuln/cmd/govulncheck@v1.8.0`; `proxy.golang.org/<module>/@v/<version>.info` for every module it added or raised | manual | `pass`: x/vuln v1.8.0 2026-09-08; MVS raised x/crypto, mod, net, sync, sys, telemetry, term, text, tools to versions from 2026-08-24 to 2026-09-08; `go mod verify`: all modules verified |
| RED | `deps_test.go`: lockfile parsing (aliases, nested copies, links, invalid JSON), `go list -m -json` (main module, replacements, local replacements), proxy path escaping, the 14-day boundary, the step on young npm and Go versions, missing publication time, a package missing from the registry, bad answers, a missing network (`not_run`), unreadable lockfile and `go list`; the updates report; audits fail on findings and do not run offline; offline and deps modes | `go test ./internal/devtool` | `fail`: `undefined: NpmLockPackages` and the others |
| RED | the updates report listed 61 entries on the repository, almost all transitive Go modules that the policy leaves to MVS; test: an indirect module is not reported, a tool's module is | same | `fail`: the indirect module was listed |
| GREEN | all | `go test ./internal/devtool` | `pass`, package coverage 100.0% |
| Real run | the repository | `go tool devtool check deps` | `DEPENDENCIES PASSED` in 9 s: 186 npm package versions and 147 Go modules at least 14 days old; `npm audit`: 0; `govulncheck`: no vulnerabilities |
| Updates out of quarantine | the report listed vite 8.3.1 → 8.3.2 and vitest, @vitest/coverage-v8 5.0.2 → 5.0.3 (published 2026-10-01 and 2026-09-30); the policy takes them in a task that changes dependencies | `npm --prefix frontend install --save-dev --save-exact vite@8.3.2 vitest@5.0.3 @vitest/coverage-v8@5.0.3` | adopted; `updates`: none left |
| Full gate | the repository | `go tool devtool check` | first run `GATE FAILED`: one uncovered block (a package missing from the registry, test added) and `single code version` reported a changed file without naming it; the step now names every changed, added or removed file (tests added). Second run `GATE PASSED` in 23 s |

The changed file of that first full run could not be identified afterwards: `npm ci`, `go list -m -u`,
`npm outdated` and `govulncheck` leave `go.sum`, `go.mod` and the lockfile unchanged, and repeated
full runs report no change. If it happens again, the report names the file.

## Stage E — documents and acceptance

| Step | Command | Result |
|---|---|---|
| Clean copy at another absolute path | every file of `git ls-files --cached --others --exclude-standard` copied into a new directory under the session scratchpad (no `node_modules`, no `build/bin`), `git init`, commit, `go tool devtool check` | `pass`: `GATE PASSED` in 26 s, exit 0; `npm ci` installed from the lockfile; bindings regenerated with no difference; the instrumented `.app` ran from the copy's own path |
| Documents | `go tool devtool check-docs`; `go tool devtool check-plan` | `pass` (128 Markdown files; plan 68 / 196 / 80 / 19, navigation regenerated by `plan-nav`) |
| Full gate on the repository | `go tool devtool check` | `pass`: `GATE PASSED` |

### Branch review and its fixes

`/branch-review` ran the three review agents on `git diff 32392e6` (all five commits and the working
tree). Verdicts: code-reviewer — 1 critical, 7 further findings, 6 warnings; tech-lead — "needs
changes" (record and workspace items, one duplicate); security-reviewer — no critical finding, 6
medium, 8 low. Every finding was fixed or recorded:

| Finding | Resolution |
|---|---|
| Task criterion "the initial commands from 003…" left unchecked | ticked; evidence below. New `check-plan` rule: a `done` task with an unchecked `- [ ]` item fails — it reported exactly this line on the real plan before the tick |
| Hooks still disabled; report spoke of the deletion in the future | file deleted, denial verified (section above) |
| Stage E exit condition (a clean review) not recorded | this section |
| Duplicate diff loop in `bindings` and `sameSources` | `changedNames` |
| `captured` stripped `pass` from CLI output | `docsCheck` / `planCheck` return a summary or problems; the CLI prints, the gate reports through `own` |
| Escape-hatch gaps | `@ts-ignore\|expect-error\|nocheck`, `.skipIf\|runIf\|fails` (also after modifiers such as `.concurrent`), `xit\|xtest\|xdescribe`, Go `//nolint` everywhere |
| Lint rules missing | `jsx-a11y` plugin; `typescript/ban-ts-comment`, `no-explicit-any`, `no-non-null-assertion` as errors. `jsx-a11y/prefer-tag-over-role` reported `<p role="status">` in `RuntimeCheck.tsx`; it is now `<output>` inside the `<p>` (same implicit `status` role, the tests' `getByRole('status')` unchanged). Control: a file with `@ts-ignore`, `any` and `a!` → three errors, exit 1; deleted |
| `kill` errors ignored in the `.app` run | reported: "pid N could not be killed: …" |
| `repositoryFiles` split on whitespace | `git ls-files -z`, split on NUL (names with spaces or Cyrillic letters stay whole) |
| `networkError` missed client timeouts | `Client.Timeout exceeded`, `context deadline exceeded`, `TLS handshake timeout` |
| `updates` reported success when it could not ask | an npm error object, a failed packument fetch or a failed `go list -m -u` → `not_run` (blocking in the full gate); an npm error that is not a network error → `FAIL` |
| `npmTimes` initialised in two places | `newChecker` |
| Paths built with `root + "/"` | `filepath.Join` |
| Security M1: here-document bodies skipped though the shell runs them (`#` comment before `<<`, a sink piped into an executor, a git alias) | `<<` in a `#` comment is not a here-document; every program reading a pipe must also be a data sink; git only as `commit\|tag\|notes\|apply\|hash-object` without `-c`/`--config-env`/`--exec-path`, gh only as `pr\|issue\|release\|api\|gist` |
| Security M2: the substitution scan misread `$(echo ")" ; …)`, `$(case … esac ; …)`, nested backticks | everything from the first substitution to the end of an unquoted body is decided too |
| Security M3: bodies decided in the starting directory, not after `cd` | decided in both |
| Security M4: Go "publication time" is the publisher-set commit time | stated in toolchain-versions and quality-gate ADRs; the guard's comment no longer calls the age check authoritative |
| Security M5: lockfile artifact not tied to the dated registry entry | each entry must be `resolved` from `https://registry.npmjs.org/` with the registry's `integrity`; a Go module replaced by a local directory fails |
| Security M6 / tech-lead: the `.app` run will open the owner's data once the app has persistence | recorded as a binding consequence in the quality-gate ADR for the first task that adds a data directory |
| Security L1: another OctopusTorrent started during the build | the running check repeats right before `open` |
| Security L2: lockfile path without `node_modules/` panicked (`slice bounds out of range`) | named by its path |
| Security L3: unbounded answers, redirects to any host | 256 MiB bound; redirects only to the same scheme and host |
| Security L5: a finding mentioning a timeout read as "network unavailable" | npm and govulncheck finding markers are checked first |
| Security L6: Go's effective configuration unchecked | new step `go env` (`go env -json`): default `GOPROXY` and `GOSUMDB`, `GOFLAGS`, `GOINSECURE`, `GONOPROXY`, `GONOSUMDB`, `GOPRIVATE` unset |
| Security L8: pre-release-labelled transitive modules in `go.sum` | one paragraph in toolchain-versions: brought by govulncheck, not selected by us, not linked |
| `GATE INCOMPLETE` exits 0 (all three reviews) | kept as approved in the plan; the reason is in the quality-gate ADR (acceptance is only the full `check`) |
| `-race` in every mode; no step time limits; Ctrl-C during the `.app` step; `testdata/` and shell inventory before 006 | recorded in the ADR and the feature doc; task 006 now points at the inventory rule |
| `System` has 13 methods; `httpClient` is a package variable | kept: one boundary for the whole tool keeps every test on one fake; the client is immutable configuration |

| Step | Command | Result |
|---|---|---|
| RED | the new cases in `frontendcov_test.go`, `appcov_test.go`, `deps_test.go` (timeouts, `updates`), `plan_test.go` | `go test ./internal/devtool` → `fail`: 5 tests — `TestEscapeHatches…` (13 missing matches), `TestAppCoverageRun…` (kill failure not reported), `TestReleaseAgeStep…` (timeout reported as FAIL), `TestUpdatesDoesNotRunWhenItCannotAsk` (4 cases `pass`), `TestCheckPlanReports…` (open item not reported) |
| RED | `TestRepositoryFilesKeepsEveryNameWhole` | `fail`: empty listing (the plain command was called) |
| RED | release-guard: 11 deny cases from the security review and one `cd` case | `fail`: 8 allowed (comment, pipe to `xargs`, git alias, `git x`, `gh x`, the three substitution forms) and the `cd` case allowed; `\|& sh` was already denied as a stdin shell |
| RED | `deps_test.go` lockfile `resolved`/`integrity`, local replace, workspace path, audit finding; `check_test.go` `go env`; `appcov_test.go` started during the build; `system_test.go` redirect and size | `fail`: build failure `undefined: CheckGoEnv` (the step did not exist) and, for the parser, `panic: slice bounds out of range [12:1]` on the workspace path |
| GREEN | all | `go test -race ./internal/devtool` → `pass`, 100.0% of statements, no zero block |
| Real registries | `go tool devtool check deps` | `DEPENDENCIES PASSED` in 8 s: every locked npm artifact is the registry's (resolved and integrity), 186 npm versions and 147 Go modules at least 14 days old |

### Final runs (hook active)

| Run | Command | Result |
|---|---|---|
| Full gate | `go tool devtool check` | exit 0, `GATE PASSED` in 21 s; 23 steps `pass`, the two not-yet-built tiers `not_run (not implemented yet)` |
| Offline | `go tool devtool check offline` | exit 0, `GATE INCOMPLETE: not run: release age, npm audit, govulncheck, updates; only go tool devtool check accepts a change` |
| Fast | `go tool devtool check fast` | exit 0, `GATE INCOMPLETE: not run: .app coverage run, go coverage, release age, npm audit, govulncheck, updates`; `go coverage` names `main.go:26.15,26.51` as covered only by the `.app` run |
| Clean copy at another absolute path | the files of `git ls-files -z --cached --others --exclude-standard` copied into the session scratchpad, `git init`, commit; then `sh .octopus/scripts/setup.sh`, `go tool devtool check`, `go tool wails doctor` | exit 0 each: setup installed from the lockfile and built `frontend/dist`; `GATE PASSED`; "Your system is ready for Wails development!". The copy was deleted. `.octopus/scripts/run.sh` (`wails dev` on the runner's port) is unchanged since its verification in a fresh worktree ([agent-tooling report](agent-tooling.md)) |

**Criterion "the initial commands from 003 are kept or moved without weakening TDD/100%":** the 003
commands are still listed under README "Individual commands"; `go test` became `go test -race` with
raw counters; the manual click-and-quit of the instrumented `.app` became the automated Apple Event
quit in every full `check`, with the merged profile required to have no zero block; real-engine and
test-bridge UI tests are printed as `not_run (not implemented yet)` instead of being claimed.

Documents changed: new ADR [quality-gate](../decisions/quality-gate.md) (O3 accepted in the
[register](../decisions/project-baseline.md#6-open-decision-register)); new feature doc
[quality-gate](../features/quality-gate.md) and the [index](../README.md); README commands section
(the gate, its modes and needs, quick module checks, individual commands); `CLAUDE.md` commands and
coverage; `/check`, `fix-checks.md`, `dependencies.md`, `review.md`; the TDD policy; the
agent-tooling and toolchain-versions ADRs.

## Source inventory (own executable code added or changed)

| File | Role | Measured by |
|---|---|---|
| `internal/devtool/check.go` | modes, step runner, report, npm cooldown, tool steps | Go coverage (unit), `check_test.go` |
| `internal/devtool/frontendcov.go` | TS inventory, raw-count coverage check, escape hatches | Go coverage (unit), `frontendcov_test.go` |
| `internal/devtool/gocov.go` | Go inventory, profile parsing and merge, `go.mod` module and tools | Go coverage (unit), `gocov_test.go` |
| `internal/devtool/appcov.go` | instrumented `.app` build, launch, Apple Event quit | Go coverage (unit with `fakeSystem`), the real run in every `check` |
| `internal/devtool/deps.go` | release age, audits, updates report | Go coverage (unit), `deps_test.go`; real registries in `check deps` |
| `internal/devtool/plan.go` | plan check and navigation | Go coverage (unit), `plan_test.go`; real plan in `cmd/devtool` |
| `internal/devtool/devtool.go`, `system.go`, `releaseguard.go`, `docs.go` | dispatch, `System` (new methods), the here-document fix, generic `sortedKeys` | Go coverage (unit), `system_test.go` against the real OS and `httptest` |
| `frontend/src/**/*.test.ts(x)` | lint fixes in tests | not own executable code (tests); lint |
| `frontend/src/features/runtime-check/RuntimeCheck.tsx` | the status line is an `<output>` (jsx-a11y) | Vitest 4 metrics, independent inventory |

Raw profile: `build/coverage/go-unit` + `build/coverage/go-app`, merged by the gate on every run;
no zero block. TS: 4 own files, 19/19 statements, 10/10 branches, 6/6 functions, 19/19 lines.

## Refactoring and debt check

- `check-docs` and `check-plan` share `listedPath` / `repositoryFiles` (`git ls-files -z`); their
  checks return a summary or problems (`docsCheck`, `planCheck`) that the gate reports through one
  `own` wrapper; `bindings` and `sameSources` share `changedNames`; `newChecker` is the one place that
  builds the checker; `sortedKeys` is generic; `substitutions` and `bodySubstitutions` share
  `expansions`; `appRunning` serves both running checks; `isDataSink` holds the here-document sink
  rule; `shellSegments` is `shellPipeline` without the pipe flags.
- The single-code-version step went from one hash to per-file fingerprints so a failure names the
  files.
- No TODOs, no ignore directives, no thresholds lowered, no skipped tests (the gate's own
  `escape hatches` step checks this).
- `release-guard` was disabled locally for this task (see above); `.claude/settings.local.json` was
  deleted before the final commit and the hook is active again (verified by a refused probe).

## Limitations

- Verified only on Apple M4 / macOS 27.0.1. The full gate needs a GUI session, the network and the
  terminal's Automation permission; it cannot run in two workspaces at the same time.
- Real-engine integration (007, 018) and test-bridge UI tests (035) do not exist yet and are printed
  as `not_run`; this task proves nothing about them.
- One unexplained `single code version` failure in the first full run (stage D) was not reproduced.
- For Go modules the release age is the tagged commit's committer time, which the publisher sets
  (security review M4); `go.sum` and `govulncheck` remain the independent controls.
- Steps have no time limit: a hung tool hangs the gate visibly (quality-gate ADR).
- The `.app` run launches the real application; before persistence exists, a task must give it an
  isolated data directory (quality-gate ADR, consequences).
- The release-guard parser stays a hand-written approximation; it fails closed in places, for example
  shell arithmetic around a command substitution is refused as "a command name taken from a
  variable".

## Summary

Task 004 is `done`: one command, `go tool devtool check`, blocks acceptance on failed tests, coverage
below exactly 100% by an independent inventory, formatting, type and lint errors, stale bindings,
broken documentation links, an inconsistent plan, young or vulnerable dependencies, and files
changing during the run, and it checks npm's and Go's effective settings and that every locked npm
artifact is the registry's. RED evidence exists for every check function, the controls fail the
gate, the three-lens review's findings are fixed test-first or recorded, and with the hook active the
gate passes on the repository and on a clean copy at another path.

## Stage log

| Stage | State |
|---|---|
| A — release-guard here-document gap | done |
| B — plan check | done |
| C0 — lint spike | done |
| C — check command and coverage inventories | done |
| D — dependency checks | done |
| E — documents, review fixes and acceptance | done |
