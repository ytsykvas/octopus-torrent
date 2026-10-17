# Report: refactor pass after task 009

**Task:** none (repository-wide refactor; owner's request of 17 October 2026)  
**Date:** 18 October 2026  
**Change version:** PR 1 `refactor/checkpoint-tests` (tests only), PR 2 `refactor/checkpoint-code`  
**Scope:** patterns, clean code and simplification across the whole repository; no product behavior
change except the deliberate ones listed below

## Session architecture plan

- **Request:** stop after task 009, review the whole application, refactor toward consistent patterns and
  clean code, and simplify instead of adding layers. Every item must be approved by a tech lead.
- **How the items were chosen:**
  1. Five reviewers, one per area, found 57 items: A engine storage, B persistence, C facade and
     frontend, D tooling, E engine test support.
  2. A tech lead per area judged each one against the code: 49 approved (12 as written, 37 with changes)
     and 8 rejected.
  3. A final tech lead settled four overlaps (A1/E1, A2/E2, A6/E11, and the child-process items A7, B5,
     E5a, E5b) and three new items.
  4. Three independent verifiers then re-read the code for every item. They confirmed every claim and
     corrected several approved changes that would have broken the build or the 100% gate: the
     `metainfo` import left by deleting `metaKey`, the `plainTorrent` close order, D1's workspace case,
     D2's miss path, and A10(c).
- **Boundaries:** no new package, no new layer, no `go.mod` change, no limit shortened, no test skipped.
  PR 1 touches tests only, so the product binary is unchanged. PR 2 changes owned code.
- **Rejected:** B6, B8, C9, D13, E4, E5b, E6, E8.
  - A shared test package across packages would need its own 100% coverage to save about 15 lines.
  - The three child-process harnesses differ for real reasons: stdin handshakes, a kill mid-work, and
    detecting a deadlock.
- **Dropped after verification:**
  - A10(a): it does not come out shorter.
  - A10(c): splitting `newGuard`'s check leaves a return no test can reach.
- **Deferred to their tasks, with a note in each task file:**
  - B11 and moving `ErrUnsupportedVersion` → 014;
  - C10 → 028;
  - the shared child-process helper → 063;
  - removing the pending-step helper with the last pending gate step → 018 or 035, whichever goes last.

## PR 1 — tests only

| Commit | Items | Change |
|---|---|---|
| `f5e7ebf` | A5 (reduced), B5, E7 | Pure moves: the engine-fact tests (findings 15–18, 20, 21, 23, 24) into `filestore/enginefacts_test.go`, the run harness into `filestore/run_test.go`, library's `TestMain` into `main_test.go`, the swarm client test next to the peer tests |
| `baf9c1b` | A1/E1, X1 | `eventually(t, limit, what, cond)` takes its deadline once. It replaces three loops that could never time out, plus four loops written in other styles. The partial-resume snapshot waits for `quiet` and `settle` instead of a fixed 300 ms |
| `ca1d7b0` | E3 | `swarm.Settle` is exported; the filestore copy of the "no piece busy" rule is a wrapper |
| `16684cb` | A2/E2, A3, A4 | `newPeer`, `plainTorrent` (its storage closes after the peer), `assertDownloadedAtMost`, `assertMarksKept`, `assertRootLost`; duplicate tests merged or split |
| `301efa8` | A6/E11, A11 (tests), E7 | `put`, `metaKey` and `hashesRight` removed in favour of existing helpers; stale names and comments fixed; sleeps named `drainWait` and `idleWindow` |
| `34e2e53` | B2, B7, B9, B10, E5a | Library: `kinds` lists every exported error, `tableAt` and `smallVolume`, Open cases left to `store_test.go`. Volume tests: one `override[T]`. The lifecycle deadlock child refuses a folder outside the temporary folder. Notes in 014, 028, 035 and 063 |

### RED / GREEN

**A1.** The loop condition at `volumes_test.go:38` was temporarily forced to be always true, and the
test was run with
`go test -count=1 -timeout 60s -run TestRefusedMarkWritesAreReported ./internal/torrentengine/filestore`:

- **Old loop:** `panic: test timed out after 1m0s` after 62 s. The loop's message never appears.
- **Over `eventually`, same breakage:** `--- FAIL … (30.06s) volumes_test.go:38: the refused write was never
  reported`.
- The breakage was reverted after both runs.

**Behavior changes in PR 1 (tests):**
- Every vanish scenario now requires all three: `ErrRootChanged` first, an unclean record, and a Seal
  error. The seeding case used to discard the Seal error.
- The partial-resume snapshot no longer races with chunks in flight.
- Peer `Close` errors now fail the engine-fact tests; they used to be discarded.

**Weakened or dropped on purpose (tech-lead review of PR 1):**
- **The merged moved-file test lost the two `os.Stat(victim)` "deciding recreated the file" checks.**
  `files.Decide` receives only a `Probe` with `Stat` and `FileSystem`, and `Ranges` is pure, so nothing on
  that path can write a file.
- **`Fatalf` became `Errorf` in two helpers, so the test goes on to the checks that follow:**
  - `assertMarksKept` in the two volume scenarios;
  - `assertDownloadedAtMost` in the partial-resume test.
  A failure still fails the test.
- **The merged Stop test no longer ends with "a write after Stop left the session clean".**
  - That check could not fail there: the timed-out Stop had already made the session unclean.
  - The refused mutation is still counted by
    `TestAfterStopReadsAreRefusedQuietlyButWritesAndMarksMakeTheSessionUnclean`.
- **The data-folder check no longer tests the prefix of the first name.** The loop right below it checks
  every name.

### Checks

- After every commit, `go tool devtool check` → `GATE PASSED`. Coverage: Go 100% of blocks (unit tests
  plus the `.app` run), TS 100%.
- `go test -race -count=1 ./internal/torrentengine/...` passes.
- The coverage of `internal/torrentengine/filestore` alone is 100.0% of statements after the test merges.
- `go test -race -count=5 ./internal/torrentengine/filestore` passes in 215 s, so no flake showed up.
- `not_run`, as before: engine integration (018) and test-bridge UI tests (035).
- **Tech-lead readiness review:**
  - verdict: ready;
  - no assertion weakened beyond the list above;
  - the three non-test Go changes are the `Settle` rename in `testutil/swarm` and a package comment.

**Debt after PR 1:** none left in the code PR 1 touched. The deferred items have owners and notes:
- B11 and moving `ErrUnsupportedVersion` → 014;
- C10 → 028;
- the shared child-process helper → 063;
- removing the pending-step helper → 018/035.

## PR 2 — owned code

| Commit | Items | Change |
|---|---|---|
| `432d555` | D4, D12 | Pure moves: `internal/devtool` gets one file per role (`workspace.go`, `docs.go`, `plan.go`, `codexagents.go`, `escapehatches.go`, `toolchain.go`); `devtool.go` keeps the entry point and the shared repository listing. The tooling documents name the new files |
| `e43f327` | D1–D3, D5–D11 | One packument cache with one store path, the lockfile parsed once, `printOwn` for the docs and plan output, `c.gomod()`/`c.module()`, named cover and lockfile paths, `npm(args...)`, the step lists joined with `slices.Concat`, `waitForPid` and `appWait`, `PlanSummary` returns only the summary |
| `75f9cc5` | A8, A9, A10(b), A11, B1, B3, B4, E9 | `NewTorrentStorage` takes a `*Completion`; `tracker.seal` calls `syncFile` itself; the guard makes its idle channel up front and Seal reads it; `Layout` returns `paths`; `SaveMarks` with a cancelled context returns `ErrCanceled`; `closeOnce`/`closeErr`; `library/doc.go` rewritten; the fixture's `truncated()` and bencode lists and maps |
| `46fadf7` | C1, C3–C8 | Every `normalized()` copies, then blanks, and a table proves every request field changes the fingerprint; `IsInfohashV1`/`V2` deleted; `ordering.ts` uses `needsFetch`; one typed `stubGoMethod` for the bridge tests; the operation test waits for `ledger.Shutdown` instead of polling; `slices.Sorted(maps.Keys(…))` |
| `9f7aa0a` | C2 | `getRuntimeInfo` follows the bridge template: `callGo` with a `runtimeInfo` decoder, returning `Result<RuntimeInfo>`. `RuntimeCheck` shows the error's fallback text. The TS test uses the Go JSON pin's values, and each test names the other |

### RED / GREEN

**B1.** `SaveMarks(cancelledCtx, nil)` was added to `TestACancelledContextIsRefusedByEveryOperation`:

- **RED:** `completion_test.go:105: SaveMarks of no marks = context canceled, want ErrCanceled`.
- **GREEN:** after `return classify(ctx.Err())` in `completion.go`, the test passes.

**C2.** `runtimeInfo.test.ts` gained a malformed answer (no `sqliteVersion`) expecting
`contract.invalidDto`, and a rejection expecting `bridge.callFailed`:

- **RED:** `npm --prefix frontend test -- src/bridge/runtimeInfo.test.ts` → 3 failures. The old wrapper
  returned the bare DTO, passed the malformed answer through, and rejected with `binding failed`.
- **GREEN:** after the decoder, the `callGo` wrapper and the component change, the runtime info, runtime
  check and `App` tests pass (8 tests).

**Behavior changes in PR 2 (owned code):**
- `SaveMarks` with a cancelled context and an empty batch returns `ErrCanceled` (B1).
- `NewTorrentStorage` accepts only `*Completion` (A9). The engine's own completion stores were never
  used in product code.
- The runtime check:
  - refuses a malformed Go answer;
  - shows the bridge's English fallback (`The application did not answer.` or `The application sent data
    this window cannot read.`) instead of `Go call failed: <reason>`;
  - never shows the diagnostic text (C2).
- The gate's `plan` step shows its problem lines with the `PLAN ` prefix, as `check-plan` and `plan-nav`
  always did; before, the step showed them bare (D5). `check_test.go` now pins the prefix. The output
  of the two commands is unchanged.

**Weakened or dropped on purpose (PR 2):**
- **`deps_test.go`: the "lockfile packages accepted invalid JSON" assertion is gone.** The `bad lockfile`
  case of the deps step still covers it.
- **`TestGetOperationReturnsTheLedgersStatus` checks only the final status.** It used to poll and check
  every intermediate status. It now waits for `ledger.Shutdown` and asserts once, so the test is
  deterministic (C6).
- **`plan_test.go` no longer asserts empty problems from `PlanSummary`.** The function no longer returns
  problems (D11).
- **`RuntimeCheck.test.tsx`: the `Error` and non-`Error` rejection tests became one error-`Result` test.**
  The component no longer sees rejections. `fromRejection` keeps its own tests in `errors.test.ts`, and
  `runtimeInfo.test.ts` checks that a rejection becomes `bridge.callFailed`.
- **`values_test.go`: the Go `IsInfohash…` checks went with the functions (C3).** The TS `ids.ts`
  decoders keep their own tests.

**Dropped after measurement:**
- **E10, the growing stack buffer in `engineStacks`.**
  - With it, `go test -race -count=10 ./internal/torrentengine` had 4 failing tests, all stall or
    metadata timing.
  - With it reverted, 10 of 10 runs passed. `main` had 1 failure in 10, in
    `TestPinnedEngineAcceptsInfoBytes…/from a peer's metadata`.
  - The extra stack captures change the timing the engine tests depend on, so the fixed buffer stays.

### Checks

- After every commit, `go tool devtool check` → `GATE PASSED`. Coverage: Go 100% of blocks (unit tests
  plus the `.app` run), TS 100% on all four metrics per file.
- `go test -race -count=20 -run 'Stop|Seal' ./internal/torrentengine/filestore` passes after A10(b).
- E9: `TestCatalogMatchesTheCommittedTestdata` passes, so the committed fixture bytes are unchanged.
- **Flakes seen, none in a package the failing commit changed:**
  - **`e43f327`, first gate:** failed in `internal/torrentengine`, where an engine test's connection stood
    still for 20 s. The gate's 30-line tail of DEBUG logs hid the test's name.
  - **`e43f327`, the separate `go test -race ./...` run after it:** failed in `internal/enginespike`
    `TestAStalledTransferIsWokenAndCounted`, with 2 recoveries where 1 was expected (an extra natural
    stall, finding 22). The test passed `-count=5` alone, and the gate rerun passed. The spike has been
    unchanged since 007 and is deleted by 034.
  - **`75f9cc5`, first gate (E10 still in):** failed in `internal/torrentengine`. This led to the E10
    measurement above.
  - **`9f7aa0a`, first gate:** failed in `swarm` `TestACorruptedPieceReceivedOverTheWireIsNeverAccepted`:
    `WaitComplete = 0 of 75000 bytes complete, missing pieces [0 1 2]`. Nothing moved, the finding-22
    stall. The test passed `-count=5` alone, and `swarm` is unchanged against `main`. The same run also
    failed `single code version`, because the report was edited during the gate. The rerun, with no
    edits, passed.
  - `TestPinnedEngineAcceptsInfoBytes…/from a peer's metadata` fails about 1 in 10 on `main` too.
  - The gate's `go tests` step should show the `--- FAIL` lines instead of the log tail. That belongs
    to its own change, not to this pass.
- `not_run`, as before:
  - engine integration (018);
  - test-bridge UI tests (035);
  - `.app` QA of the new runtime-check error text. The gate still launches the `.app` for coverage.
- **Tech-lead readiness review:**
  - Verdict: ready after fixes. All the fixes were in this report: the D5 wording, the flake attribution,
    and the list of weakened or dropped tests.
  - It found no behavior change beyond the deliberate ones.
  - A10(b) is race-free: `idle` is never reassigned and is closed only after `flight.Wait()`.
    `-race -count=10` of the Stop and Seal tests passed again.
  - C1 keeps the JSON byte-identical, because the only extra copied field is unexported.
  - Its optional suggestions were taken: the gate test pins the `PLAN ` prefix, `NewTorrentStorage` states
    that the completion must not be nil, and the desktop shell names its last verified version.
  - Left for the owner: the `updates` step lists `github.com/anacrolix/chansync` v0.7.0 → v0.8.0 as out of
    quarantine. This pass changes no `go.mod`.

**Debt after PR 2:** none in the code this pass touched. The deferred items keep the owners listed in
the plan above.
