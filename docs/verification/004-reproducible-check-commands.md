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

## Stage log

| Stage | State |
|---|---|
| A — release-guard here-document gap | done |
| B — plan check | not started |
| C0 — lint spike | not started |
| C — check command and coverage inventories | not started |
| D — dependency checks | not started |
| E — documents and acceptance | not started |
