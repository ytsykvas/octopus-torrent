# Report 012: macOS Trash and path protection

**Task:** [012](../../product/tasks/012-macos-trash-and-path-spike.md). **Requirements:** `DEL-02`, `DEL-04`, `STACK-01`. **ADR:** [trash-and-paths](../decisions/trash-and-paths.md). **Branch:** `task/012-macos-trash`.

## Session architecture plan

Approved by the owner on 2026-10-19.

### Context

012 is the last P1 spike. Before any delete feature (DEL-01–05) is built, the project must prove one
supported way to move a payload file to the macOS Trash — reversible, per file, no shell (STACK-01) —
and one safe way to confine a physical file operation to the folder a torrent owns against symlinks,
traversal and a file swapped between the check and the act. The engine, SQLite and the filesystem do
not share a transaction, so a Trash failure must leave an explicit per-file result and never fall
back to irreversible deletion (DEL-04). Dependencies 003 and 007 are `done`.

**Owner decisions (2026-10-19):** the Trash API is `NSFileManager trashItemAtURL` via cgo (the only
supported reversible per-file API, no shell; the first cgo in the repo, and Wails already links
AppKit); no new dependency. No `.app` QA — the real API is proven by a cgo integration test on a
temporary disk image; the full UI→Go→Trash flow is `not_run` until the delete feature.

### Design

New real package `internal/platform` (kept product code, the home the architecture maps for native
capabilities). A one-function cgo seam behind an interface, so the orchestration and every safety
branch are ordinary Go tested with a fake, and the real API is proven by two integration calls.

- `Trasher interface { TrashFile(absPath string) (newLocation string, err error) }`.
- `Result{ Name string; Trashed bool; NewLocation string; Err error }`, one per requested name (030
  records intent before and outcome after; the file op and the record are separate results).
- `Trash(saveDir string, names []string, t Trasher) ([]Result, error)`: refuse an unsafe `saveDir`
  (a volume root or the home, by `files.IdentityOf`) before touching anything; open `saveDir` with
  `os.OpenRoot` so a name escaping the folder is refused by the standard library (the hard
  guarantee); per name, open through the root with `O_NOFOLLOW`, `fstat` for identity, refuse a
  non-regular file, trash `filepath.Join(saveDir, name)`, record the result; the aggregate error
  names exactly the files that remain (DEL-04). Never `os.Remove`.
- `trash_darwin.go` (`//go:build darwin`): `systemTrasher`/`NewSystemTrasher` with one
  `NSFileManager trashItemAtURL:resultingItemURL:error:` call.

Reuse: `internal/files` (`Stat`, `IdentityOf`); `internal/testutil/volume` (a temporary `hdiutil`
disk image) so real-API test files land in that volume's `.Trashes`, never the owner's `~/.Trash`.

### Boundaries proven and not supported

- Hard guarantee: nothing outside the torrent's folder is trashed (structural, `os.OpenRoot`).
- Each file is opened `O_NOFOLLOW` and must be a regular file, so a symlink or a directory put in the
  name's place is refused. A regular→regular same-name swap inside the folder between the check and
  the move is a residual inherent to the path-based Trash API (no fd-based trash on macOS), documented
  for the partial-result journal (032).
- `.app` UI flow: `not_run`.

## RED → GREEN

| Step | Command | Result |
|---|---|---|
| orchestration RED | `go vet ./internal/platform` with `trash_test.go` only | `fail` as expected: `undefined: Trash` |
| orchestration GREEN | `go test -race ./internal/platform` (fake `Trasher`, `t.TempDir`) | `pass`; two branches (an f.Stat error folded into the regular-file check; a non-directory folder) covered by a directory and a regular-file-as-folder case; 100% |
| real API | `go test -race ./internal/platform` (cgo, APFS disk image) | `pass`; a file on the image is trashed to the volume's `.Trashes` (not `~/.Trash`), a missing path returns the `NSError`, and `Trash` drives the real `NSFileManager` end to end on a volume subfolder |
| review rewrite | `go test -race ./internal/platform` after the three-agent review | `pass`, 100% Go: the confinement escape (C1), the volume-root miss (M1), the FIFO/symlink handling (L1/L2) and the invalid-path process abort (M3) are fixed and tested |
| gate | `go tool devtool check` | `GATE PASSED`, 100% Go and TS, and `trash_darwin.m` 100% via the native coverage step (069) |

Commands, working directory `/Users/tsykvas/projects/torrent`, data: synthetic files in `t.TempDir()`
and on a temporary `hdiutil` APFS image; `-race`; exit 0.

## Boundaries confirmed / not supported

- `pass` — only the named files reach the Trasher; a neighbour is untouched (DEL-02).
- `pass` — a name with `..` or an escaping symlink is refused by `os.OpenRoot`; nothing outside the
  folder is trashed (the hard guarantee).
- `pass` — a directory / non-regular file, and a symlink, are refused (`O_NOFOLLOW` + regular check).
- `pass` — a Unicode name (NFC and NFD) is passed through; APFS keeps the one file.
- `pass` — a Trasher error and a missing file leave the file on disk, named in `ErrSomeRemain`; no
  `os.Remove` anywhere (DEL-04).
- `pass` — a volume root, the home and a non-directory folder are refused before any file is touched
  (`ErrUnsafeFolder`).
- `pass` — the real `NSFileManager` call moves a file to the volume's Trash and surfaces its error.
- `not_run` — the full UI→Go→Trash flow (no delete UI yet; owner decision). Proven at the API level.
- Not supported: a regular→regular same-name swap inside the folder between the check and the move
  (path-based Trash API; residual documented for 032).

## Coverage

100% of `internal/platform`'s own **Go** statements (`go tool devtool check`). The fake-`Trasher`
tests give deterministic 100% of the orchestration, the seams (`openRoot`, `fdPath`, `mountPoint`)
cover the syscall error paths, and `systemTrasher`'s Go wrapper is covered by the real cgo tests
(success, a missing file, a bad path).

The Objective-C body (`trash_darwin.m`) is measured by the `native coverage` gate step
([069](../../product/tasks/069-native-code-coverage-gate.md), clang `-fcoverage-mapping` + `llvm-cov`)
at 100% (9/9 regions), satisfying the policy ([TDD_AND_QUALITY §3](../engineering/TDD_AND_QUALITY.md),
"New own code in another language requires a measurement tool before it is accepted"). 012 was
delivered together with 069. (An earlier draft of this report wrongly claimed the policy permits an
unmeasured adapter; it does not.)

## Refactoring and debt

- The confinement escape the security review reproduced (`filepath.Join` diverging from `os.Root`
  on `..` after an in-root symlink) is closed: names must be `filepath.IsLocal`, and the move acts on
  the opened file's own path (`fcntl F_GETPATH`), not on its name.
- The volume-root backstop now detects a real mounted volume (Statfs `f_mntonname`), not only `/`,
  and requires a clean absolute folder; it fails closed.
- Non-regular names (symlink, FIFO, device) are refused by `Lstat` before any open, so a FIFO cannot
  block; `O_NONBLOCK` is set too.
- A bad path (empty, relative, NUL, non-UTF-8) to the native `Trasher` is refused in Go, so the
  NSString/NSURL call cannot abort the process.
- Reused `internal/files.IdentityOf` and `internal/testutil/volume`. No new dependency, no `TODO`,
  no skipped test, no lowered threshold.
- Debt left to later tasks, not this one: the TOCTOU residual and the partial-result journal (032),
  folder selection and collision reporting (020), and wiring `Trash` into the facade with a delete UI.
