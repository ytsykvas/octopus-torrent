# macOS Trash and confined path deletion

**Status:** `accepted` (the Trash mechanism and path confinement; the `.m` is covered by the native coverage gate step, [069](../../product/tasks/069-native-code-coverage-gate.md)). The partial-result journal is [032](../../product/tasks/032-safe-payload-trash-journal.md), folder selection and collisions [020](../../product/tasks/020-destination-filesystem-service.md)  
**Date:** 2026-10-19  
**Task:** [012](../../product/tasks/012-macos-trash-and-path-spike.md)  
**Register item:** O8 ([baseline](project-baseline.md#6-open-decision-register))  
**Requirements:** `DEL-02`, `DEL-04`, `STACK-01`

## Question

Before any delete feature (DEL-01–05), the project needs one supported way to move a payload file to
the macOS Trash — reversible, per file, never irreversible — and one safe way to confine a physical
file operation to the folder a torrent owns, against `..`, absolute paths, symlinks and a file
swapped between the check and the act. React must not run a shell or delete through a workaround
(STACK-01). The engine, SQLite and the filesystem do not form one transaction, so a Trash failure
must leave an explicit per-file result and never fall back to `rm`.

## Decision

A new real package **`internal/platform`** (macOS only; the home the architecture maps for native
capabilities) with a one-function native seam behind an interface.

- **The Trash API is `NSFileManager trashItemAtURL:resultingItemURL:error:`**, called through cgo
  (`trash_darwin.m`, linked with `-framework Foundation`). It is the only supported API that moves an
  item to the Trash (reversible), one call per file, returning the item's new in-Trash location or an
  `NSError`. This is the **first cgo in the repository**; Wails already links AppKit for the GUI, so
  no new toolchain requirement. No shell, no `osascript`, no third-party module, and never `os.Remove`
  as a fallback.
- **The native call is one interface method**, `Trasher.TrashFile(absPath) (newLocation, error)`, so
  the orchestration and every safety branch are ordinary Go tested with a fake, and the real API is
  proven by two cgo integration calls. `NewSystemTrasher()` is what the facade wires.
- **Confinement is structural.** `Trash(saveDir, names, Trasher)` opens `saveDir` with `os.OpenRoot`
  (Go 1.27) and resolves every name inside it; a name that leaves the folder is refused by the
  standard library. This is the **hard guarantee**: nothing outside the torrent's folder is ever
  trashed. Each file is opened with `O_NOFOLLOW` and checked to be a regular file, which rejects a
  symlink or a directory put in the name's place.
- **An unsafe folder is refused first** (`ErrUnsafeFolder`): `saveDir` must be a directory that is
  not a volume root (its own parent, by device and inode) and not the user's home. The real authority
  is the folder a torrent owns (009's `Layout`); this is a backstop so a too-broad selection cannot
  sweep a volume.
- **The result is per file.** `Trash` returns one `Result{Name, Trashed, NewLocation, Err}` for each
  name and, if any stayed, `ErrSomeRemain` naming exactly those (DEL-04). The caller records the
  intent and this outcome separately from any SQLite write; a crash between the move and the record
  leaves an undetermined result that 032 reconciles — the file op and the record are not one
  transaction.

## Alternatives considered

| Option | Why not |
|---|---|
| `osascript`/Finder to move to Trash | A shell workaround (STACK-01 forbids it); no robust per-file result |
| A third-party Go trash module | Needs a dependency and its 14-day cooldown, and most wrap the same cgo call or shell out; the direct call is a few lines |
| `os.Remove` / `rm` | Irreversible; the requirements demand the Trash, and never presenting deletion as "moved to Trash" |
| Resolve by path with `os.Lstat` then act | A path check is a TOCTOU of its own; `os.OpenRoot` + `O_NOFOLLOW` confine and type-check the opened object atomically within the root |

## Consequences

- `internal/platform` is real product code; the facade (a later task) calls `Trash` with the torrent's
  own save folder and a `NewSystemTrasher`. The product links cgo/AppKit, which the Wails build
  already does; `product isolation` is unaffected (platform is not spike/testutil/engine).
- **TOCTOU residual:** `trashItemAtURL` re-resolves the path, so a regular file swapped for another
  regular file of the same name inside the folder between the check and the move cannot be
  distinguished by a path-based API. Confinement bounds the blast radius to the torrent's own folder
  and the `O_NOFOLLOW` open rejects a symlink/non-regular swap; the regular→regular residual is left
  for the delete coordinator/journal (032) and stated here.
- **Not supported / `not_run`:** the full UI→Go→Trash flow (no delete UI yet) is `not_run`, proven at
  the API level by the cgo disk-image test; 020 adds folder selection and collision reporting; 032
  adds the partial-result journal.
- **Native-code coverage** is provided by [069](../../product/tasks/069-native-code-coverage-gate.md)
  (clang source coverage + `llvm-cov` in the gate), which brings `trash_darwin.m` to 100%; 012 was
  completed together with it, satisfying the policy ([TDD_AND_QUALITY §3](../engineering/TDD_AND_QUALITY.md)).

## Verified grounds

[Report 012](../verification/012-macos-trash-and-path-spike.md): Apple M4, macOS 27, Go 1.27.2. The
orchestration and every safety branch are covered in pure Go with a fake `Trasher` on synthetic data
in a temporary directory; the real `NSFileManager` call is exercised on a temporary `hdiutil` disk
image (`internal/testutil/volume`), so the trashed test files land in that volume's `.Trashes` and
vanish when it is detached — the owner's `~/.Trash` is never touched. 100% of the package's own Go
statements, `-race`; `trash_darwin.m` at 100% by the native coverage step (069).
