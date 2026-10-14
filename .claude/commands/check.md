Run every OctopusTorrent health check, report the results, and offer to fix what failed.

Until task 004 replaces this list with a single local command, these checks are the gate. Run them
from the repository root, in order, and **never stop early** — a later check often explains an earlier
failure. All seven block a commit.

## 1. Frontend dependencies and types
```bash
npm --prefix frontend ci
npm --prefix frontend run typecheck
```

## 2. Frontend tests with 100% coverage per file
```bash
npm --prefix frontend run test:coverage
```
The thresholds in `frontend/vite.config.ts` are 100 for lines, statements, functions and branches,
per file. A failure is a missing test, never a reason to touch the thresholds.

## 3. Frontend build (needed by the Go tests: `main.go` embeds `frontend/dist`)
```bash
npm --prefix frontend run build
```

## 4. Go formatting, analysis and tests
```bash
gofmt -l .
go vet ./...
go test ./...
```
`gofmt -l .` must print nothing.

## 5. Go coverage — raw counters, 100%
```bash
C="$PWD/build/coverage"; rm -rf "$C"; mkdir -p "$C/go-unit"
go test -cover -coverpkg=./... ./... -args -test.gocoverdir="$C/go-unit"
go tool covdata textfmt -i="$C/go-unit" -o "$C/go.out"
awk 'NR>1 && $NF==0' "$C/go.out"
```
The `cmd/devtool` test runs its instrumented binary into the same directory. The only line allowed
in this output is `main.go`'s `main()`, which runs only inside the real `.app`; for a full 100% run
the merged-profile procedure in the [README](../../README.md) (it launches the instrumented app). Report
anything else as a failure.

## 6. Generated bindings in sync
```bash
go tool wails generate module
git -c core.fileMode=false status --short frontend/wailsjs
```
No output means the committed bindings match the Go facade. `core.fileMode=false` because Wails
itself disagrees on modes: `generate module` writes `wailsjs/runtime/*` as 755, `wails build` as 644;
only the content matters.

## 7. Documentation links and instruction pointers
```bash
go tool devtool check-docs
```
Every relative Markdown link and anchor must resolve, and every `.claude/**.md` path named in the
agent and runner configuration must exist — runner configs and agents point at
`.claude/docs/actions/*` by path, and a renamed file makes them read nothing, silently. The Codex
agent mirrors are checked by `go test` in step 4 (regenerate with `go tool devtool codex-agents`).

## Report

| Check | Status | Issues |
|---|---|---|
| Frontend deps + types | pass/FAIL | … |
| Frontend tests + coverage | pass/FAIL | … |
| Frontend build | pass/FAIL | … |
| Go fmt / vet / test | pass/FAIL | … |
| Go coverage | pass/FAIL | uncovered blocks |
| Bindings | pass/FAIL | … |
| Docs and pointers | pass/DEAD | … |

"All checks pass" means all seven passed. For each failure: `file:line`, what, why. If something is
fixable, ask: "Found N issue(s). Fix them? 1. Yes 2. No" and wait. Fixing follows
[fix-checks.md](../docs/actions/fix-checks.md). If everything passes, say so in one line.
