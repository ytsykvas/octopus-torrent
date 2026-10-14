# Report 001: initial scope and open-decision register

**Task:** [001](../../product/tasks/001-scope-and-decision-register.md)  
**Date:** 14 October 2026  
**Change version:** the first repository commit (task 002); before 001, Git did not exist  
**Result:** [docs/decisions/project-baseline.md](../decisions/project-baseline.md)

## Session architecture plan

- **Actual state:** `/Users/tsykvas/projects/torrent` was empty, without Git; the documentation existed only in `~/Desktop/OctopusTorrent`. There is no executable code.
- **Impact:** documentation only. Module boundaries, contracts and the stack do not change; the architecture does not need an update, because 001 records the already agreed map (architecture §3) as the future one.
- **Reuse:** the requirement/scenario matrices and the condensed table of open decisions in `tasks/README.md` are the source of responsible tasks; the baseline links to them instead of copying 80 rows.
- **Checks before the change (RED):** absence of the 001 results; counting of IDs/scenarios; link check.
- **Documentation:** the new `docs/decisions/project-baseline.md`, this report, the `docs/README.md` index, the status in the task and in the navigation.

## RED: expected state before the change

| Check | Command | Result |
|---|---|---|
| The 001 result is absent | `ls docs/decisions/project-baseline.md docs/verification/001-scope-and-decision-register.md` | `fail` (expected): `No such file or directory` for both |
| The documentation copy is identical to the source | `rsync -a --exclude=.DS_Store ~/Desktop/OctopusTorrent/ ./ && diff -rq -x .DS_Store ~/Desktop/OctopusTorrent .` | `pass`: no differences (`COPY OK`) |
| Links in the specifications | `python3 -I check_links.py .` (the session's link-check script; moving it into the repository is task 004) | initial run after the baseline draft: 1 `fail`, this report not yet created |

## GREEN: actual checks

| # | Check | Command / method | Result |
|---|---|---|---|
| 1 | Unique IDs in the requirement tables | `grep -oE '^\| [A-Z]+-[0-9]{2} \|' PROJECT_REQUIREMENTS.md \| tr -d '\| ' \| sort -u \| wc -l` | `pass`: **80** (ADD 8, AI 10, AUTO 2, COL 1, DATA 3, DEL 5, NEXT 2, QA 8, REL 3, SET 8, STACK 5, TR 8, UI 17) |
| 2 | The "Full coverage of 80 requirements" matrix = the set of requirements | `diff` of the sorted requirement IDs and the matrix rows of `tasks/README.md` | `pass`: they match, 80 = 80 |
| 3 | All IDs are mentioned in tasks; no invented IDs | `comm` of the IDs from `tasks/*.md` (in backticks) and the requirement IDs | `pass`: the difference is empty in both directions |
| 4 | Any mentions of IDs in the requirements outside the tables | `grep -ohE '\b[A-Z]+-[0-9]{2}\b'` compared with the table set | `pass`: no new IDs |
| 5 | Gaps in numbering | enumeration of the numbers of each prefix | recorded: ADD-05, UI-05, UI-14 are absent in the source |
| 6 | 19 end-to-end scenarios | `grep -c '^[0-9]\{1,2\}\. \*\*' PROJECT_REQUIREMENTS.md`; `AS-NN` rows in the matrix | `pass`: 19 and 19 |
| 7 | 68 tasks, all `planned` at the start | `ls tasks/[0-9]*.md \| wc -l`; `grep -h '^\*\*Status:\*\*'` (the label was Ukrainian when this ran) | `pass`: 68, all `planned` |
| 8 | The open parameters of requirements §7 have an owner | manual mapping of the 6 items of §7 and the tasks/README table to the register O1–O18 | `pass`: §7 item 1→O1, item 2→O2, item 3→O6, item 4→O9, item 5→O4/O5, item 6→O18 |
| 9 | Environment | `sw_vers; uname -m; sysctl -n machdep.cpu.brand_string hw.memsize; df -h; xcode-select -p; clang --version; git/node/npm/sqlite3/gh/brew --version; command -v go wails` | recorded in baseline §5; `go`, `wails` are absent |
| 10 | Personal limits without sign-in, payments, cloud and built-in AI | manual review of requirements §1, §3, §4, §5.2, the concept, architecture §1 | `pass`: the limits are confirmed by links in baseline §2 |
| 11 | Comparison of the specifications with the mockup | review of `MOCKUP.html` against the requirements and the concept | results in baseline §4 |
| 12 | Links and anchors in all Markdown files | `python3 -I check_links.py .` | `pass` (final run: see "Summary") |
| 13 | The read-only sources are unchanged | `diff -rq` with `~/Desktop/OctopusTorrent` after the changes | `pass`: only the files deliberately changed by 001/002 differ (see "Summary") |

**Code coverage:** `not_applicable`: there is no owned executable code.

## REFACTOR and debt check

- The inventory of IDs and responsible tasks does not duplicate the `tasks/README.md` matrices: the baseline contains a summary and links. The condensed table of open decisions in the plan remains an overview; the baseline register adds the check and the decision document.
- The R2 discrepancy (the mockup name) is fixed in the requirements as a verifiable link change. R1 is not fixed by renumbering; the explanation is in the task's context handoff.
- There is no new technical debt; no TODOs were added. The open decisions O1–O18 have owners and are not debt of 001.

## Summary

Final run (after 001 and 002): link check `pass`, 1692 relative links in 83 Markdown files, 0 errors. `diff -rq` with `~/Desktop/OctopusTorrent` shows only deliberate changes: changed `PROJECT_REQUIREMENTS.md` (R2), `AGENTS.md`, `docs/README.md`, `tasks/README.md`, `tasks/001…`, `tasks/002…`; added `README.md`, `.gitignore`, `docs/decisions/{project-baseline,_ADR_TEMPLATE}.md`, `docs/verification/00{1,2}-…md`. `archive/`, `MOCKUP.html`, the concept, the architecture, the research and tasks 003–068 are unchanged.

The 001 criteria are met: 80 IDs and 19 scenarios are listed and reconciled, discrepancies are recorded, every open decision has a task, a criterion, a check and a document, the personal limits are confirmed, the code directory and the next task (002) are defined, facts are separated from assumptions.

Next task: [002](../../product/tasks/002-repository-and-agent-entrypoint.md). Nothing can be executed independently of 002: 003 requires 002 and installed Go and Wails CLI.
