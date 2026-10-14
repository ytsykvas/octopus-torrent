# Report 002 — Git repository and AI entry point

**Task:** [002](../../product/tasks/002-repository-and-agent-entrypoint.md)  
**Date:** 14 October 2026  
**Change version:** first commit of the `main` branch  
**Environment:** Apple M4, macOS 27.0.1, git 2.54.0, python3 (system) for link checking

## Session architecture plan

- **Actual state:** after 001 the root `/Users/tsykvas/projects/torrent` contains only documentation; there is no Git; there is no root README, `.gitignore` or ADR template. The existing `AGENTS.md` and `docs/` are filled in, not scaffolding.
- **Impact:** only documentation and declarative Git configuration. The architecture does not change: README and AGENTS.md link to §2–§4 of the architecture instead of duplicating them.
- **Reuse:** the structure of future directories comes from architecture §3; the session rules in AGENTS.md are kept verbatim, new sections only add module boundaries, bindings and task handoff; ADR names come from the baseline register.
- **Decision on bindings:** `frontend/wailsjs/` is tracked in Git (not ignored), so that TS checks work from a clone and regeneration is verified by the absence of a diff (the final command comes in 004). `build/darwin/` and `build/appicon.png` are Wails package configuration and are also tracked; only `build/bin/` is ignored.
- **Checks before the change (RED):** absence of Git and files; `.gitignore` check on sample paths.

## RED — expected state before the change

| Check | Command | Result |
|---|---|---|
| No repository | `git status` | `fail` (expected): `fatal: not a git repository` |
| No entry point files | `ls README.md .gitignore docs/decisions/_ADR_TEMPLATE.md` | `fail` (expected): all three `No such file or directory` |
| Ignore rules | `check_ignore.sh .` after `git init`, before `.gitignore` (session script: `git check-ignore --no-index` for 21 paths that must be ignored and 11 that must be tracked) | `fail` (expected): 21 FAIL — no working file was ignored |

## GREEN — actual checks

| # | Check | Command / method | Result |
|---|---|---|---|
| 1 | Repository created in the agreed location, not nested | `git init -b main`; `git rev-parse --show-toplevel` | `pass`: `/Users/tsykvas/projects/torrent`, parent directories without Git |
| 2 | `.gitignore` on paths (check-ignore) | `check_ignore.sh .` (extended to 35 samples, including `internal/library/testdata/corrupt.db`, `testdata/.DS_Store`) | `pass`: 35/35 |
| 3 | `.gitignore` on **real** files | `check_ignore_real.sh .gitignore <scratch>`: creates 35 files in a temporary repository, `git add -A`, compares staged | `pass`: 22 ignored (build, `node_modules`, `*.db`/`*.sqlite*` with WAL/SHM/journal, coverage, logs, `tmp/`, `.DS_Store`, personal `*.torrent`, `downloads/`, `.env`, `.vscode/`); 13 added (`testdata/**` together with `.torrent`, `.db`, `.log`; sources; lockfile; `go.sum`; `frontend/wailsjs/`; `build/darwin/`) |
| 4 | Links and anchors in all Markdown | `python3 -I check_links.py .` | `pass`: 0 errors (see "Summary" for the final number) |
| 5 | Route README → requirement → task → module → next task | manual walk: README "Directory map" → TR-01 → matrix → [025](../../product/tasks/025-pause-resume-application.md) → `internal/app`/`internal/torrentengine`; README "Current state" → [003](../../product/tasks/003-wails-macos-build-spike.md) | `pass` |
| 6 | Future distinguished from existing; commands not called working | README review: sections "Created now" / "Not yet created"; "No command works or has been verified yet" | `pass` |
| 7 | Independence of scripts from the absolute path recorded | README "Build and check commands", AGENTS.md | `pass` |
| 8 | AGENTS.md: module boundaries, bindings, check and handoff order, no references to the chat | review; existing content kept unchanged (only appended) | `pass` |
| 9 | Git index without working DBs, logs, personal data and the developer's absolute paths in code files | `git ls-files`; `git ls-files \| grep -E '\.(db\|sqlite\|log\|torrent)$'`; `git grep -n '/Users/'` | `pass` (see "Summary") |
| 10 | Read-only sources not changed | `diff -rq -x .DS_Store -x .git ~/Desktop/OctopusTorrent .` | `pass` (see "Summary") |

**Code coverage:** `not_applicable` — there is no own executable code. The check scripts of this session live outside the repository as one-off verification tools; a permanent documentation and `.gitignore` check with tests and coverage is created by [004](../../product/tasks/004-reproducible-check-commands.md).

## REFACTOR and debt check

- The initial version of `.gitignore` allowed only `testdata/**/*.torrent`; the artificial corrupted DB for [016](../../product/tasks/016-migration-backup-corruption-recovery.md) would have been ignored. Replaced with the general exception `!testdata/**`; `.DS_Store` stays ignored there too; checks repeated (rows 2–3).
- README and AGENTS.md do not copy the specifications: boundaries are described in a table with links to the architecture.
- No new technical debt; no TODOs added.

## Summary

Final run: links — 1692 in 83 files, 0 errors; `.gitignore` — 35/35 (check-ignore) and 35/35 (real files); `git rev-parse` in the parent directory — `not a git repository` (no nesting); staged files contain no `.db`/`.sqlite`/`.log`/`.torrent`/`.DS_Store`. `git grep '/Users/'` finds the absolute path only in documents where it is recorded as the agreed repository location (baseline §8, verification reports, task results); it is absent from scripts and configuration. Changes relative to the Desktop source are only the planned ones (list in [report 001](001-scope-and-decision-register.md#summary)).

The criteria of 002 are met. Next task — [003](../../product/tasks/003-wails-macos-build-spike.md): first install Go and Wails CLI and record the versions (O2).
