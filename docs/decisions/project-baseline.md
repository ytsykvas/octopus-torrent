# Project baseline and open-decision register

**Status:** accepted, task [001](../../product/tasks/001-scope-and-decision-register.md), 14 October 2026.  
**Evidence:** [docs/verification/001-scope-and-decision-register.md](../verification/001-scope-and-decision-register.md).  
**Nature of the document:** organizes the already agreed scope. It does not change the stack, requirements or product boundaries and does not add new capabilities.

Reliability markers in this document:

- **Verified** — the fact was established by a command or a count during 001; the command is given in the verification report.
- **Agreed** — an owner decision recorded in the specifications; not yet proven experimentally.
- **Open** — a spike or a decision is needed in the named task; until then any value is only an assumption.

## 1. Requirement sources and order of authority

Agreed ([tasks/README.md](../../product/tasks/README.md), section "Stack, scope and source of truth"):

1. [PROJECT_REQUIREMENTS.md](../../product/PROJECT_REQUIREMENTS.md) — behavior, criteria, 80 requirement IDs and 19 end-to-end scenarios.
2. [PRODUCT_CONCEPT.md](../../product/PRODUCT_CONCEPT.md) — idea, personal limits, implementation order.
3. [APPLICATION_ARCHITECTURE.md](../../product/APPLICATION_ARCHITECTURE.md) — components, DTO, lifecycles, data.
4. [ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md](../../product/ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md) — engine API and assumptions not yet proven by a prototype.
5. [MOCKUP.html](../../product/MOCKUP.html) — reference for styling and interactions; demo toggles are not the product.
6. [TDD_AND_QUALITY.md](../engineering/TDD_AND_QUALITY.md), [AGENTS.md](../../AGENTS.md), [docs/README.md](../README.md) — mandatory rules for every session.
7. [tasks/README.md](../../product/tasks/README.md) and numbered tasks — the way of execution, not a change of requirements.

[archive/LIBTORRENT_TECHNICAL_RESEARCH.md](../../product/archive/LIBTORRENT_TECHNICAL_RESEARCH.md) — only a historical reference about the rejected stack (libtorrent + SwiftUI).

## 2. Agreed scope and personal limits

Agreed:

- A personal local desktop BitTorrent client for macOS; the first target is the owner's own Apple Silicon Mac ([requirements §1](../../product/PROJECT_REQUIREMENTS.md#1-goal-and-boundaries-of-the-personal-version)).
- Stack: React + TypeScript strict / Vite, Wails v2 (stable branch), Go, anacrolix/torrent, SQLite through a repository. Wails v3 is not part of the current decision ([concept](../../product/PRODUCT_CONCEPT.md), "Chosen stack").
- **None of:** accounts and sign-in, Free/Pro plans, subscriptions, payments, license server, cloud sync, own server, product analytics, public website ([requirements §1](../../product/PROJECT_REQUIREMENTS.md#1-goal-and-boundaries-of-the-personal-version), DATA-01, DATA-02; [architecture §1](../../product/APPLICATION_ARCHITECTURE.md#1-stack-and-system-boundary)).
- **No built-in AI model.** AI-first means readable code and reproducible context in the repository for developer agents ([requirements §5.2](../../product/PROJECT_REQUIREMENTS.md#52-ai-first-code-for-further-ai-work), [tasks/README.md](../../product/tasks/README.md)).
- Not part of the agreed scope: content catalog, TV streaming, web version, mobile apps, library sharing between Macs, advanced network analytics, releases for other platforms ([requirements §4](../../product/PROJECT_REQUIREMENTS.md#4-planned-after-mvp), [concept](../../product/PRODUCT_CONCEPT.md), "Stages").
- The network is needed only for BitTorrent; the list and settings are available offline (DATA-01).

## 3. Requirement inventory

Verified: the tables of `PROJECT_REQUIREMENTS.md` contain **80 unique IDs**; the matrix ["Full coverage of the 80 requirements"](../../product/tasks/README.md#full-coverage-of-the-80-requirements) has the same set, and each ID has a primary task; each ID is mentioned in at least one task, and tasks do not reference non-existent IDs.

| Requirements section | Prefix | Count | ID |
|---|---|---|---|
| 2.1 Adding | ADD | 8 | ADD-01–04, ADD-06–09 |
| 2.2 Downloading and seeding | TR | 8 | TR-01–08 |
| 2.3 List and details | UI | 17 | UI-01–04, UI-06–13, UI-15–19 |
| 2.4 Deletion | DEL | 5 | DEL-01–05 |
| 2.5 Settings | SET | 8 | SET-01–08 |
| 2.6 Distribution | REL | 3 | REL-01–03 |
| 2.7 Library and automation | COL, AUTO | 1 + 2 | COL-01, AUTO-01–02 |
| 3 Data and privacy | DATA | 3 | DATA-01–03 |
| 4 After MVP | NEXT | 2 | NEXT-01–02 |
| 5.1 Stack contracts | STACK | 5 | STACK-01–05 |
| 5.2 AI-first | AI | 10 | AI-01–10 |
| 5.3 Engineering discipline | QA | 8 | QA-01–08 |
| **Total** | | **80** | |

The numbering gaps **ADD-05, UI-05, UI-14** exist in the source; do not fill them or renumber.

**End-to-end scenarios.** Verified: section 6 of the requirements has **19** items; the matrix ["Coverage of the 19 end-to-end scenarios"](../../product/tasks/README.md#coverage-of-the-19-end-to-end-scenarios) contains AS-01…AS-19 with implementation and acceptance tasks. AS-NN denotes item N of section 6, not new requirement IDs.

The responsible tasks for each ID and scenario are not duplicated here: their source is the matrices in `product/tasks/README.md`.

## 4. Discrepancies between documents

Recorded explicitly; none was resolved silently in favor of the more convenient option.

| # | Discrepancy | Sources | Resolution |
|---|---|---|---|
| R1 | Task 001 requires "all 72 IDs", while the requirements and plan contain 80. | [001](../../product/tasks/001-scope-and-decision-register.md), "Work order" item 1 and criteria; [tasks/README.md](../../product/tasks/README.md#check-of-the-plan-itself) | Cause: QA-01–QA-08 were added in revision 3 of the plan (72 + 8 = 80); the text of 001 was not updated. Authoritative requirements (source 1) → the valid number is **80**. A note was added to the context handoff of 001; numbering was not changed. |
| R2 | The requirements refer to the mockup as `OctopusTorrent-mockup.html`; the actual file is `MOCKUP.html`. | [Requirements](../../product/PROJECT_REQUIREMENTS.md), line "Related materials" | The actual file name was corrected in the link line (a verifiable link change, not a behavior change). |
| R3 | The concept lists search and filters among the "Core capabilities", while the requirements assign them to NEXT-01/02 "after MVP". | [Concept](../../product/PRODUCT_CONCEPT.md), "Core capabilities"; [requirements §4](../../product/PROJECT_REQUIREMENTS.md#4-planned-after-mvp) | Not a scope contradiction: both sources keep them in the project. The MVP boundary is defined by the requirements — NEXT-01/02 in phase P6 ([055](../../product/tasks/055-search-status-filters.md)), after [054](../../product/tasks/054-global-upload-ui-mvp-gate.md). |
| R4 | Architecture: the `.app` registers `.torrent` **and magnet**; requirements §5 mention only `CFBundleDocumentTypes` for `.torrent`. | [Architecture §9](../../product/APPLICATION_ARCHITECTURE.md#9-macos-and-frontend); [requirements §5](../../product/PROJECT_REQUIREMENTS.md#5-technical-and-quality-requirements), ADD-02 | ADD-02 requires system opening of magnet links, so URL scheme registration is also needed. Open for [036](../../product/tasks/036-finder-magnet-open-queue.md): the `magnet:` registration mechanism is checked in the built `.app`. |
| R5 | Two different Ukrainian terms, "Bin" (title of 012) and "Trash" (tasks/README, architecture §8), for the macOS Trash. | [012](../../product/tasks/012-macos-trash-and-path-spike.md); [architecture §8](../../product/APPLICATION_ARCHITECTURE.md#8-files-and-safe-deletion) | Synonyms for the same system mechanism. The UI term is fixed by localization ([041](../../product/tasks/041-six-languages-formatters.md)); documents are not renamed in 001. |

| R6 | The MVP requirements UI-07 ("search does not lead to an action on hidden entries") and UI-16 (switching between "All / Downloading / Completed / Seeding") rely on search and sections that the requirements assign to NEXT-01/02 after MVP. | [Requirements §2.3](../../product/PROJECT_REQUIREMENTS.md#23-list-and-details) and [§4](../../product/PROJECT_REQUIREMENTS.md#4-planned-after-mvp) | Interpretation pending the owner's decision: in the MVP the UI-07/UI-16 criteria apply to the sections and search that already exist; full acceptance of these parts happens together with [055](../../product/tasks/055-search-status-filters.md). Scope unchanged. |
| R7 | Filter name: the requirements say "active", the mockup and UI-16 say "Downloading"; in the mockup "Completed" contains only the `done` status, so a completed torrent that is seeding does not appear there. | NEXT-02, UI-16; `MOCKUP.html` (sidebar sections, `render`) | Open for [013](../../product/tasks/013-transfer-policy-state-machine.md)/[055](../../product/tasks/055-search-status-filters.md) (O11): selection rules and section names. |

### Mockup versus specifications

Verified by a full review of `MOCKUP.html` (498 lines; now `product/MOCKUP.html`) with selective cross-checking of lines. The mockup is not a source of behavior (authority §1): where it diverges from the requirements, the requirements are carried over.

| # | What the mockup does | What the specification requires | Kind | Task that carries over the requirement |
|---|---|---|---|---|
| M1 | Magnet accepts any non-empty string and immediately creates an active entry with ready files (`openMagnet`/`addPending`). | ADD-02, ADD-04, TR-06, AS-03: waiting for metadata, file selection after it, an invalid link does not create an entry. | contradiction (demo) | 022, 044 |
| M2 | Re-adding the "example" creates a duplicate of the existing entry. | ADD-03. | contradiction (demo) | 021 |
| M3 | Only 4 statuses (Downloading, Paused, Seeding, Completed); there is no "waiting for metadata" and no error of an individual torrent. | UI-01, UI-10. | contradiction | 013, 027, 045, 051 |
| M4 | Single deletion with files does not show the file list; group deletion shows only names without paths. | DEL-02, DEL-05. | contradiction | 049 |
| M5 | A new entry shows "0 B of 2.1 GB"; the MB/GB rule is followed only in the details. | UI-12. | contradiction | 041 (O12) |
| M6 | Settings have no speed limits. | TR-04. | missing capability | 026, 050 |
| M7 | The initial language is `uk`. | SET-06: language from macOS preferences, otherwise English. | contradiction | 041 |
| M8 | Warning about disabling upload: "trackers may not count downloads". | SET-08: "may apply their own rules". | text inaccuracy | 050, 053 |
| M9 | A skipped completed file has the text "Not selected"; "Skipped" is only for incomplete ones. | AS-07: "Skipped". | inaccuracy | 046 |
| M10 | During search "Select all" becomes "Select shown". | UI-07 does not describe such a state. | inaccuracy | 048, 055 |
| M11 | The full path in the preview is available only via `title`, not in the checkbox's accessible name. | ADD-09: full path for VoiceOver. | accessibility contradiction | 044 |
| M12 | No tags, collections, rules or watch folder. | COL-01, AUTO-01, AUTO-02. | missing capability (agreed for P6) | 056–061 |

**Mockup demo elements that are not carried over into the product.** Named in requirements §4: "Restore example", "Show error example", "Show offline state". Additionally found (not named in the requirements, also not carried over): the "About mockup" button in details; "View example" in the add dialog and in the error alert; limits "for the mockup" (20 MB file, 1000 files); manual path entry and `webkitdirectory` instead of the system dialog (SET-01 → 037); service notes "Mockup…/In the web mockup…"; storing settings in `localStorage` (in the product — SQLite, SET-02); the "Interactive mockup" footer.

**Not defined by the specification, remains an implementation decision:** hiding the drop zone at widths ≤ 860 px; the hint "Files stay with you" below the list (UI-17 forbids hints only above the list).

**Matches the requirements:** UI-03, UI-06, UI-07 (persistent checkboxes, panel after selection), UI-08, UI-09, UI-10 (alert placement), UI-11, UI-12 (path, percentage position), UI-17, UI-18, UI-19, TR-07, ADD-06–09 (except M11), DEL-01, SET-04, SET-07, SET-08 (toggle and preservation of manual pauses). There are no out-of-scope functions (accounts, payments, subscriptions, licenses, cloud, analytics, AI, streaming, catalog) in the mockup.

## 5. Development environment

Verified on 14 October 2026 (commands are in the 001 verification report):

| Parameter | Value |
|---|---|
| Mac | Apple M4, `arm64`, 16 GiB RAM |
| macOS | 27.0.1 (build 26A434) |
| Free space | ≈ 32 GiB of 228 GiB (data volume 84 % full) |
| Xcode | Command Line Tools only (`/Library/Developer/CommandLineTools`), Apple clang 21.0.0; full Xcode is not active |
| Git | 2.54.0, `init.defaultBranch=main` |
| Node.js / npm | v26.0.0 / 11.12.1 |
| Homebrew | 7.0.6 |
| sqlite3 CLI | 3.54.0 (system; this is not the SQLite the application will be built with) |
| gh CLI | 2.96.0 |
| **Go** | **not installed** |
| **Wails CLI** | **not installed** |

Consequences:

- Missing Go and Wails CLI are a prerequisite for [003](../../product/tasks/003-wails-macos-build-spike.md): install them and record the versions before the RED step of 003. Not needed for 001/002.
- The free ≈ 32 GiB are enough for development and small controlled torrents; large spike payloads in 064 need a separate space check.
- **Open:** the minimum supported macOS version is not defined. The verified configuration (above) ≠ the minimum supported one; decision — [003](../../product/tasks/003-wails-macos-build-spike.md)/[066](../../product/tasks/066-personal-build-install-license-audit.md). Support for other Macs only on separate need (requirements §7 item 1).

## 6. Open-decision register

Each decision is recorded as a separate ADR in `docs/decisions/` following the template from task 002, with evidence and consequences. The ADR names below are planned; the files do not exist yet. If a choice changes agreed behavior or the stack, an owner decision is needed first.

| # | Question | Owner task | Selection criterion | Check | Decision document |
|---|---|---|---|---|---|
| O1 | Minimum macOS, architecture of the target Mac (requirements §7 item 1, §5 "Compatibility") | [003](../../product/tasks/003-wails-macos-build-spike.md), [066](../../product/tasks/066-personal-build-install-license-audit.md) | Real build and launch on the owner's own Mac; no assumptions about other Macs | The `.app` opens on the target system; `LSMinimumSystemVersion` matches the verified one | [macos-target.md](macos-target.md) — **accepted** in 003 (only 27.0.1 arm64 verified) |
| O2 | Versions of Go, Wails v2 patch, anacrolix/torrent tag, Node.js, frontend dependencies (§7 item 2) | [003](../../product/tasks/003-wails-macos-build-spike.md), [004](../../product/tasks/004-reproducible-check-commands.md), [007](../../product/tasks/007-engine-api-lifecycle-spike.md) | One consistent API set; clean reproducible build from `go.mod`/`go.sum` and lockfile | Build from a clean clone; signatures cross-checked on the pinned tag (the research mentions Wails v2.15.0 — not yet verified) | [toolchain-versions.md](toolchain-versions.md) — **accepted** in 003 for Go/Wails/Node/frontend; anacrolix — 007 |
| O3 | Test tools and 100% coverage for Go/TS (Vitest, coverage provider, analyzers) | [004](../../product/tasks/004-reproducible-check-commands.md) | Exact 4 TS metrics and Go statements per file, full source inventory | The local command fails at < 100 % or an incomplete inventory | [quality-gate.md](quality-gate.md) — **accepted** in 004: `go tool devtool check`, Vitest + coverage-v8 5.0.3, Go raw counters with the `.app` run, Oxlint + tsgolint |
| O4 | SQLite driver (CGO / pure Go), bundled SQLite, WAL, durability, pool (§7 item 5) | [008](../../product/tasks/008-sqlite-driver-durability-spike.md), [014](../../product/tasks/014-sqlite-schema-migrations.md) | Actual `sqlite_version()`, commit/checkpoint/cancel, build in `.app` without Homebrew | Spike 008 on the owner's own Mac | `docs/decisions/sqlite-driver.md` |
| O5 | Engine storage backend, payload location, durable piece completion (§7 item 5) | [009](../../product/tasks/009-storage-and-completion-spike.md) | Physical paths inside the save path; restart/crash/corruption without a hidden memory fallback | Controlled torrents from 006, restart and completion corruption | `docs/decisions/engine-storage.md` |
| O6 | Global upload disabling without stopping downloads, confirmation boundary and timeout (§7 item 3, TR-08) | [010](../../product/tasks/010-upload-stop-and-pause-spike.md), [053](../../product/tasks/053-global-upload-policy-production.md) | Local and remote counters: upload stopped, download continues; residual buffer measured | Controlled swarm on the pinned engine version | `docs/decisions/global-upload-stop.md` |
| O7 | Temporary magnet storage and transition to the final folder | [011](../../product/tasks/011-magnet-metadata-storage-spike.md), [022](../../product/tasks/022-pending-import-coordinator.md) | Payload forbidden until confirmation; a borrowed handle does not Drop; final folder proven | Metadata-only spike with cancellation | `docs/decisions/magnet-pending-storage.md` |
| O8 | macOS Trash mechanism, path checks, collisions, shared files | [012](../../product/tasks/012-macos-trash-and-path-spike.md), [020](../../product/tasks/020-destination-filesystem-service.md), [032](../../product/tasks/032-safe-payload-trash-journal.md) | Moving specifically to the Trash; re-checking every file; journal of partial results | Spike on artificial files in the built `.app` | `docs/decisions/trash-and-paths.md` |
| O9 | Moved, changed or partially lost payloads (§7 item 4) | [030](../../product/tasks/030-relink-missing-payload.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md) | Explicit re-selection and check, no automatic moving | Restore scenarios on controlled data | `docs/decisions/missing-payload-recovery.md` |
| O10 | Backup, migrations and DB corruption | [016](../../product/tasks/016-migration-backup-corruption-recovery.md) | Consistent copy of the active DB, rollback of a failed migration without losing the library | Migration and artificial corruption tests | `docs/decisions/db-backup-recovery.md` |
| O11 | Status priority, filter intersection, zero wanted | [013](../../product/tasks/013-transfer-policy-state-machine.md), [027](../../product/tasks/027-metrics-status-snapshots.md), [055](../../product/tasks/055-search-status-filters.md) | Explicit table of states and selection rules | Table-driven policy tests | `docs/decisions/transfer-status-model.md` |
| O12 | Decimal/binary units, small files, rounding (UI-12) | [041](../../product/tasks/041-six-languages-formatters.md) | MB/GB rule, small files, precision of large numbers | Formatter tests for 6 languages | `docs/decisions/size-units.md` |
| O13 | Registration of `magnet:` in the bundle (see R4) | [036](../../product/tasks/036-finder-magnet-open-queue.md) | Cold and repeated launches receive the link | Check of the built `.app` | `docs/decisions/finder-magnet-registration.md` |
| O14 | Tags: uniqueness, ANY/ALL, deleted references | [056](../../product/tasks/056-local-tags-crud.md), [057](../../product/tasks/057-smart-collections.md) | Stable IDs, visible conditions, no file side effects | Repository and UI tests | `docs/decisions/tags-collections.md` |
| O15 | Rule conflicts, matching, manual selection | [058](../../product/tasks/058-rules-semantics-preview-evaluator.md), [059](../../product/tasks/059-rules-crud-import-application.md) | Preview without side effects, visible order, re-check before commit | Evaluator and import tests | `docs/decisions/import-rules.md` |
| O16 | Watch folder: subfolders, file stability, candidate retries | [060](../../product/tasks/060-watch-folder-stable-detection.md), [061](../../product/tasks/061-watch-folder-settings-queue-ui.md) | Parse/restat/reconcile; originals preserved; auto-add in the paused state only explicitly | Tests with controlled file writes | `docs/decisions/watch-folder.md` |
| O17 | Load, update budgets, list virtualization | [064](../../product/tasks/064-scale-profiling-lifecycle-soak.md) | Measurements on the owner's own Mac; optimization only of a proven problem | Profiling and soak | `docs/decisions/performance-budgets.md` |
| O18 | Installation, signing, updating and rollback of the personal build; license audit (§7 item 6, REL-02) | [066](../../product/tasks/066-personal-build-install-license-audit.md), [067](../../product/tasks/067-owner-help-upgrade-recovery-runbook.md) | An actual personal build without dependence on Homebrew; compatible data restore | Installation on the owner's own Mac; license inventory (MPL-2.0 anacrolix and others) | `docs/decisions/personal-install-signing.md` |

The ["Open decisions"](../../product/tasks/README.md#open-decisions-and-their-responsible-tasks) table in the plan contains the same questions (O1–O2, O4–O9, O11–O12, O14–O18) in condensed form; this register adds the check and the decision document and the new items O3, O10, O13.

## 7. Division of work

Agreed ([tasks/README.md](../../product/tasks/README.md#phases-and-transition-criteria)):

| Stage | Tasks | What it proves |
|---|---|---|
| Start | 001–002 | Scope, decision register, repository and AI entry point. |
| Risk prototypes | 003–012 | `.app`, commands, DTO, test torrents, early end-to-end path 007, SQLite, storage, upload, magnet, Trash. |
| **Interim check of core transfer** | **052** | A narrow persisted scenario: import, transfer, pause, restart — without group deletion, Finder and Trash. |
| **Full MVP** | **054** | All MVP capabilities, including Finder/magnet, file deletion and independent upload disabling (010 → 053). |
| **Agreed next capabilities** | **055–062** | Search and filters (NEXT-01/02), tags and collections (COL-01), rules and watch folder (AUTO-01/02). Not part of the first MVP, but remain in the overall plan. |
| Completion | 063–068 | Reliability, profiling, accessibility, personal build, help, handoff from a clean context. |

## 8. Code directory and file boundaries

- **Root of the project and of the future Git repository:** `/Users/tsykvas/projects/torrent` (verified: at the time of 001 it was not a Git repository and is not located inside another one). Task 002 initializes the single repository here; nested repositories are not created.
- **Origin:** the documentation was copied on 14 October 2026 from `~/Desktop/OctopusTorrent` (content identical, verified with `diff -rq`). The working copy is only the project root; the Desktop copy is not synchronized and is not a source of truth. On 15 October 2026 the repository copy was translated into English ([repository-language](repository-language.md)); the Desktop copy stays Ukrainian.
- **Read-only:** `product/archive/` (history of the rejected stack). There are no synchronized `sources/` in the project. Specifications (requirements, concept, architecture, research) change only deliberately in the task they concern, with the reason recorded.
- **Mockup:** `product/MOCKUP.html` — a styling reference; code from it is not considered tested, and porting to React goes through TDD.
- **Future code:** the structure from [architecture §3](../../product/APPLICATION_ARCHITECTURE.md#3-repository-structure-for-ai-first) (`main.go`, `internal/…`, `frontend/…`, `scripts/`). None of these paths has been created yet; the actual paths are confirmed by 003.
- **Documentation:** `docs/` per [docs/README.md](../README.md); decisions — `docs/decisions/`, evidence — `docs/verification/`, features — `docs/features/`.

## 9. Facts, assumptions and future checks

| Category | Content |
|---|---|
| Verified facts | 80 IDs and 19 scenarios; completeness of the matrices; 68 tasks in status `planned` at the start of 001; Mac and tool configuration; absence of Go/Wails; identity of the documentation copy. |
| Agreed but not proven | Stack and product boundaries; engine behavior from the anacrolix research (in particular `DisallowDataUpload()`, file priorities, storage); DTO formats and architecture lifecycles. |
| Open | O1–O18 above. |
| Not applicable | Code coverage: there is no executable code yet (`not_applicable`). |
