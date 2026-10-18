# 019 — Parse metadata and build a consistent file tree

**Status:** `planned`  
**Phase:** P3 — Import, transfer and data safety  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [018](018-torrent-engine-adapter.md), [006](006-controlled-torrent-fixtures.md)  
**Requirements:** `ADD-01`, `ADD-04`, `ADD-09`, `UI-18`, `STACK-01`

## Outcome

Create a single verified model of a torrent's real files, from which the tree, selection totals and mappings to engine indices are built.

## Read before starting

- [Task execution rules](README.md): start and completion conditions, statuses and evidence of completion.
- [Requirements](../PROJECT_REQUIREMENTS.md): `ADD-01`, `ADD-04`, `ADD-09`, `UI-18`, `STACK-01`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory preceding tasks. If the required evidence is missing, the dependent part of the work must not start.

## Work order

1. Read metainfo within the set size limits and keep the original bytes of the `info` dictionary.
2. Build the tree for v1, v2 and hybrid torrents without a duplicated root; check size arithmetic for overflow.
3. Do not show padding service files in the interface, but keep the exact mapping to the file indices in the engine.
4. Check path components and the limits on depth, count and size; treat names as untrusted text.
5. For corrupted or unsupported content, return a typed error before a library entry is created.

## Artifacts and change locations

**Proposed locations:** `internal/app/metainfo; canonical file model`. This is a map of future code; after the project skeleton is created, verify the actual paths and update the map in the architecture.
**Evidence of completion:** an entry `docs/verification/019-metainfo-parser-file-tree.md` or an exact link to an existing verification or build report in the handoff summary. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The tree, the sums of selected bytes and the mappings to engine indices come from one consistent model.
- [ ] Invalid metadata does not create a library entry.
- [ ] Zero-length files are handled without division by zero.
- [ ] All checks listed below are performed at the appropriate level; checks that were not run or failed are not marked as successful.

## Checks

- Parse a deep tree, identical names in different folders and Unicode names; verify the structure and index mappings without losing files.
- Pass an inconsistent hybrid description, corrupted hash layers, overflowing values and unsafe paths; every unacceptable input must be rejected before an entry is created.

**How to run:** use the actual commands recorded in [004](004-reproducible-check-commands.md) and the root README after the project skeleton is created. Until these commands exist, the checks remain a plan. Record the exact command, working directory, test data, configuration, exit code and result. Run system scenarios in the built `.app`; mark interface checks with test doubles and checks of the real engine separately.

## Decisions, risks and boundaries

All names and paths come from untrusted metadata. An interface check does not replace parsing, resource limits and path checks in Go. Transforming the model must not change the original bytes on which the torrent's identity depends.

**Basis for technical decisions:** [Metainfo API](https://pkg.go.dev/github.com/anacrolix/torrent/metainfo), [BEP52](https://www.bittorrent.org/beps/bep_0052.html). During implementation, cross-check signatures and limitations against the pinned version. Reading API documentation does not replace a performed integration check.

**From 005** ([api-contract](../../docs/decisions/api-contract.md)): clean torrent file paths in Go (relative, no `..`, no empty segments, no control characters) before they enter a DTO; the TypeScript decoders accept the text as is.

**From 006** ([controlled-swarm](../../docs/decisions/controlled-swarm.md)): parse the committed fixtures in `internal/torrentengine/testutil/fixture/testdata` (`fixtures.json` lists purpose, hashes and the engine's stage for each). The engine **accepts** `invalid-hybrid-mismatch` (v1 list disagrees with the file tree) and `invalid-path-traversal` (`..`): our parser must reject both. `invalid-pieces-length` and `invalid-negative-length` pass the engine's parsing and fail only on add. Adding a torrent to the engine already truncates existing files at its zero-length paths (`invalid-empty-traversal`, `invalid-embedded-separator`, a name collision), so reject `..`, `.`, empty components and components containing a separator before the engine sees the metainfo; compare cleaned joined paths, not components.

**From 007** ([engine-lifecycle](../../docs/decisions/engine-lifecycle.md)): reject a v2-only metainfo (no v1 infohash) with a clear error before any engine call — the owner's decision of 2026-10-16, while the pinned engine panics on it. A `.torrent` file's infohash is computed from its own info bytes, so finding 11 (mismatching info bytes are accepted) cannot arise here; it matters where a stored infohash meets separately sourced bytes (018, 023, 029).

**From 011** ([magnet-pending-storage](../../docs/decisions/magnet-pending-storage.md), [feature](../../docs/app/magnet-metadata.md)): add a `.torrent` with no peers until `SetInfoBytes` has returned: once a peer's metadata set the info, `SetInfoBytes` returns nil and keeps the peer's info (finding 31). A magnet's verified `metainfo.Info` comes from `Pending.Info()`; apply the same validation limits to it before the confirmation dialog shows it.

## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; set `done` only after the criteria are met and the evidence is recorded.
- Record a short result: what was changed, exact checks with statuses `pass`, `fail` or `not_run`, ADRs, actual change locations in the code and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
