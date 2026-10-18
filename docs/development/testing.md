# Testing

> Last updated: 2026-10-16

Every change goes RED → GREEN → REFACTOR, and all own executable code stays at **100%**. The policy
is in [TDD_AND_QUALITY](../engineering/TDD_AND_QUALITY.md); this page explains how the tests are
built and run. Only `go tool devtool check` proves coverage ([quality gate](../tooling/quality-gate.md));
do not reconstruct it by hand.

## Go

- Tests live next to the code (`*_test.go`, same package) and run with `go test -race`.
- **No windows in unit tests.** `run(start, stderr, ledger)` takes the Wails start function, so
  `main_test.go` passes a fake and inspects the `options.App`. `internal/devtool` reaches the OS only
  through `devtool.System` (`fakeSystem` in tests); `cmd/devtool`'s tests build the real binary with
  `GOCOVERDIR` and run it (including the Codex-mirror and plan checks of this repository).
- **Concurrency:** ledger tests run 20 concurrent duplicates under `-race`. Goroutine order is set
  with explicit seams, not sleeps: the ledger's `spawn` delays an operation's start.
- **Coverage:** the gate collects raw counters from `go test -covermode=atomic` plus an
  instrumented `.app` run, because `main()` runs only in the real app. Every own file with a
  function must be measured, and no block may be zero. `check fast` reports `main()` as `not_run`.

## Engine tests: own torrents and a local swarm

Anything that needs the real torrent engine uses the [test torrents and the local swarm](../tooling/test-torrents.md):

- `fixture.Named("hybrid-tree")` gives deterministic payload bytes and metainfo, with the expected
  hashes in `fixtures.json`; `fx.WritePayload(dir)` writes the files where the engine expects them.
- `swarm.NewPeer(t.TempDir(), …)` starts an anacrolix client on 127.0.0.1 with every discovery path
  off; peers meet only through `swarm.Connect`.
- **Every wait has a deadline** (`context.WithTimeout`): `WaitComplete` returns an
  `IncompleteError` listing the missing pieces instead of hanging. `t.Skip` is forbidden, so these
  tests run in every gate mode, offline included (loopback is not the network).
- Close every peer in `t.Cleanup`; on a failure the helper prints both engines' logs.
- Engine timing is handled in the harness (`swarm.Settle`, `Refuses`); do not read piece state right after
  `VerifyDataContext` or assume a port is free right after `Client.Close`.
- Swarm tests are not `t.Parallel`: `WaitEngineStopped` needs the only engine in the process.
- A package that starts engine clients uses `func TestMain(m *testing.M) { os.Exit(swarm.Main(m)) }`:
  it refuses an environment that changes the engine and fails the package if an engine goroutine is
  left.
- The engine contract tests of 007 (`internal/torrentengine/lifecycle_test.go`) prove the pinned
  engine's API, lifecycle and findings with the raw client (`Peer.Client()`). When the engine is
  upgraded, any of them that turns red means a recorded behavior changed: decide it again in the
  [engine-lifecycle ADR](../decisions/engine-lifecycle.md).
- A behavior that hangs the engine (finding 9) is proved in a child process of the test binary, never
  in the test process itself.

## TypeScript

- Vitest with jsdom (`src/test/setup.ts` adds jest-dom matchers and, after each test, cleans up and removes
  any `window.go` stub; `src/test/goBinding.ts` has `stubGoMethod` for a Go facade method).
- **Inventory:** every `frontend/src/**/*.ts(x)` except tests, `src/test/` and `.d.ts`. The gate
  derives it from the repository listing, not from `vite.config.ts`, and requires
  covered == total on lines, statements, functions and branches for every file.
- **Components:** Testing Library, queries by role and label; the bridge function comes in as a
  prop (`vi.fn`).
- **Bridge wrappers:** stub `window.go.app.App.<Method>` and assert the `Result`
  ([calling Go](../bridge/calling-go.md#testing-a-wrapper)).

## Shared contract examples

`internal/app/contracts/testdata/*.json` and `manifest.json` are one contract for both languages:

| Direction | Go checks | TypeScript checks |
|---|---|---|
| `toFrontend`, valid | decode → encode gives the same JSON exactly | the decoder accepts it; 64-bit values are exact `bigint`s |
| `toFrontend`, invalid | — | the decoder rejects it at `path` |
| `toGo`, valid | `Validate()` accepts it | the cases listed in `commands.test.ts` produce their examples exactly |
| `toGo`, invalid | `Validate()` → `request.invalid` with `field` and `reason` | — |

`vocabulary.json` lists every enumeration and error code. Both suites compare their lists with it.
TypeScript reads the files through `src/test/contractExamples.ts` (`example`, `record`,
`decodedExample`, `manifest`).

## Library tests: real SQLite

Anything that touches the library database uses [the library store](../app/library-store.md) on
real SQLite, never a fake:

- Open a store in `t.TempDir()`; never a path under the user's folders, and never the default data
  directory.
- Prove a guarantee with the real failure: another process holding the lock, a child process killed
  with SIGKILL, a read-only folder (`chmod 0500`, restored in `t.Cleanup`), a real full volume
  (`hdiutil` disk image, detached in `t.Cleanup`). A seam is for a failure SQLite cannot be made to
  produce.
- A refusal must leave the file as it was: compare its SHA-256 and inode before and after.
- A volume for a test (`internal/testutil/volume`): `volume.New` attaches a small `hdiutil` image (APFS,
  HFS+, ExFAT or MS-DOS; an ExFAT label has at most 11 characters), `Detach` pulls it, `Attach` plugs it
  again, `AttachAt` plugs it at another folder (another drive in place of the pulled one), `Fill` fills it
  to the last byte. Close every store opened on a volume before it goes away: SQLite
  keys its locks by device and inode, and a new volume can get the same numbers. A package that attaches
  volumes uses `volume.Main` in `TestMain`, which fails on an image left by an ended run.
- Engine tests that write files run under classic file IO: `swarm.MainClassicIO` in `TestMain`.
  A peer of a transfer that must complete uses the product's keep-alive interval
  (`swarm.Options{KeepAliveTimeout: transferswitch.WakeInterval}`): with the engine default a fresh
  transfer stands still now and then under `-race` (findings 22 and 26, [global-upload-stop](../decisions/global-upload-stop.md)).
- Statements run through `library.Tx`, so they honour the operation's context.

## Commands

```sh
go test -race ./internal/app/... -run TestName -v
go test -race -count=1 ./internal/torrentengine/...                 # fixtures, the swarm, engine contract, storage and upload-stop tests, about 90 s
go test -race -count=1 ./internal/enginespike ./cmd/engine-spike     # the engine spike app's Go side
go test -race -count=1 ./internal/library                           # real SQLite: durability, locks, read-only, full disk, about 8 s
go test ./internal/torrentengine/testutil/fixture -args -update     # rewrite the committed torrents
npm --prefix frontend test -- src/bridge/contract/errors.test.ts -t 'unknown code'
npm --prefix frontend run test:coverage     # all files, per-file thresholds
go tool devtool check fast                  # everything except the .app run and the network
```

## Lint

- Oxlint, type-aware (`npm --prefix frontend run lint`): correctness and suspicious categories as
  errors, plus `no-explicit-any`, `no-non-null-assertion`, `ban-ts-comment`, `no-unsafe-type-assertion`,
  `consistent-function-scoping` (the last two through the `suspicious` category) and the React hooks
  rules. `perf` is `warn`, and `--deny-warnings` turns warnings into failures.
- Use type guards instead of `as`. Where TypeScript cannot follow a proven fact, use one documented
  `asserts` function (see `struct` in `decode.ts`).
- Go: `gofmt` and `go vet`.

## Forbidden

The gate fails on any of these:

- lowering thresholds or narrowing the inventory;
- coverage ignore comments (`v8|c8|istanbul ignore`), lint disables, `@ts-ignore` / `@ts-expect-error` / `@ts-nocheck`, `//nolint`;
- skipped or conditional tests (`.skip`, `.only`, `.todo`, `skipIf`, `runIf`, `fails`, `xit`, `t.Skip`).

Tests use only synthetic data in temporary directories or `testdata/`, never the owner's
torrents or downloads. Go code under `testdata/` fails the gate (`go test ./...` never builds it):
generators live in packages, `testdata/` holds only data.
