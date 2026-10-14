# 006 — Create controlled torrents and a local swarm

**Status:** `planned`  
**Phase:** P1 — Prototypes of risky contracts  
**Priority:** critical path of the main scenario or completion  
**Size:** One complete behavior or module; if needed, split an independently verifiable part into a separate task  
**Hard dependencies:** [004](004-reproducible-check-commands.md)  
**Requirements:** `AI-08`, `TR-02`

## Outcome

Prepare own reproducible test torrents and a local transfer environment between known peers. Subsequent engine tests must work without public trackers and third-party content, with measurable bytes and controlled completion.

## Read before starting

- [Task execution rules](README.md): task start and completion conditions, statuses and check evidence.
- [Requirements](../PROJECT_REQUIREMENTS.md): `AI-08`, `TR-02`, the corresponding criteria and end-to-end scenarios.
- [Architecture](../APPLICATION_ARCHITECTURE.md): module boundaries, DTO, lifecycles and data.
- [Concept](../PRODUCT_CONCEPT.md): personal limits and implementation order.
- [Engine technical plan](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md): API and assumptions not confirmed by a prototype.
- Results and ADRs of all mandatory prerequisites above; do not start the dependent part without the required evidence.

## Work order

1. Create small deterministic files and a metainfo generator for v1, v2 and hybrid. Cover a single file, a nested tree, repeated names in different folders, a zero-length file, pad files and v1 pieces that cross the boundary between two files.
2. Add separate invalid metainfo and payload variants with corrupted bytes. Keep the expected content, full infohashes, file hashes and the purpose of each example in a short description of the test data.
3. Prepare local seeder and leecher with different temporary directories and an explicit peer list. Disable public DHT, tracker discovery and other unwanted paths out to external peers; test metadata must not contain third-party web seeds.
4. Record the configuration, transport, addresses of local peers and the payload counters of both sides. Provide measurement of the start of transfer, its stop and residual buffered traffic for subsequent policy tests.
5. For every wait, set a time limit, support cancellation and guaranteed shutdown of clients, goroutines, listeners and processes. Clean up only the temporary directories created by this test; on error, keep sufficient diagnostics.

## Artifacts and change locations

**Proposed locations:** `internal/torrentengine/testutil; testdata/torrents; scripts/`. This is a map of future code; after the initial setup, verify the actual paths and update the architecture if needed.
**Evidence of completion:** an entry `docs/verification/006-controlled-torrent-fixtures.md` or an exact link to a test or build report in the context handoff. One report can be reused if it proves exactly these criteria.

## Acceptance criteria

- [ ] The generator re-creates the same metadata and expected hashes from the same test data.
- [ ] After local transfer, the received files match the expected ones byte for byte, and their checksums match.
- [ ] Configuration and observation confirm that the test transfer uses only the specified local peers and does not depend on public discovery or trackers.
- [ ] Absence of peers, a corrupted piece and cancellation give a predictable result within the time limit.
- [ ] Two consecutive runs finish without leftover processes, open listeners or temporary directory conflicts; user files are not changed.

## Checks

- Generate the examples twice and compare the metainfo, full infohashes and payload checksums.
- Perform a successful transfer of a single-file and a multi-file torrent; compare the result with the source bytes and the counters of both peers.
- Separately check corrupted metadata, a corrupted piece, absence of peers and cancellation during transfer. Make sure no scenario waits indefinitely.
- Run the environment twice in a row, verify that resources are released and review the diagnostics for third-party addresses and personal data.

**How to run:** add the exact commands to the [004](004-reproducible-check-commands.md) set and the README. For each run, record the example, configuration, time limit, exit code and result. These are checks of the real engine in a controlled environment; a browser and a Wails window are not needed for them.

## Decisions, risks and boundaries

The metainfo generator and the test environment must use the pinned API version and explicitly check support for each format. Do not confuse service network traffic with payload. A missing response from a public tracker cannot be the cause of a flaky test, because the test must not use one. Timeouts, cancellation and cleanup are part of the result of this task, not optional auxiliary work.

**Basis for technical decisions:** [BEP52](https://www.bittorrent.org/beps/bep_0052.html), [anacrolix API](https://pkg.go.dev/github.com/anacrolix/torrent). Cross-check signatures and limitations against the pinned version; reading the API is not a passed integration check.


## Mandatory TDD cycle and architecture review

At the start of each session, follow the [agent rules](../../AGENTS.md): read the current architecture and feature documentation, find code to reuse, record the plan of the change and the required refactoring. This also applies to continuing this task.

1. **RED:** before the new implementation, run the test/check of the expected result and record the proper reason for failure. For pure refactoring, first confirm the guarantees of the existing behavior.
2. **GREEN:** minimal correct implementation, tests and the required regression; **100% of all own executable code** under the [exact metrics](../../docs/engineering/TDD_AND_QUALITY.md), without narrowing the inventory or ignore workarounds.
3. **REFACTOR:** immediately remove duplication and wrong boundaries in the affected scenario, reuse shared modules; repeat the green checks and 100% coverage.
4. Update the feature document and the [docs/ index](../../docs/README.md) in the same change; record significant architecture decisions in an ADR.

**Completion is blocked** if the required tests/coverage/documentation are missing or technical debt remains. For documentation and declarative files, their structural/contract checks are performed; absence of executable code is not called 100%. In the task report, record the architecture conclusion, RED/GREEN, the refactoring performed, the inventory/profile and the check for absence of debt. The rules also apply to prototypes.

## Handoff to the next executor

- Update the status in this file and in the README navigation table at the same time; `done` only after the criteria and verification evidence.
- Record a short result: what was changed, exact checks with pass/fail/not_run, ADRs, actual code paths and known limitations.
- For `blocked`, name the specific reason, the missing evidence/decision, the way to unblock and the independent tasks that can continue.
- Update the documentation, contract and README if the API, structure or build commands changed; do not leave required context only in the chat.
