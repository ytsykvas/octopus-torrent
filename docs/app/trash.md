# Moving payload to the Trash, confined to the torrent's folder

**Status:** `implemented` (prototype `internal/platform`; 100% Go and `trash_darwin.m` 100% via the native coverage gate step, task [069](../../product/tasks/069-native-code-coverage-gate.md)); the facade wires it in a later delete task  
**Requirements:** `DEL-02`, `DEL-04`, `STACK-01`  
**Related tasks:** [012](../../product/tasks/012-macos-trash-and-path-spike.md); next [020](../../product/tasks/020-destination-filesystem-service.md) (folder selection, collisions), [032](../../product/tasks/032-safe-payload-trash-journal.md) (partial-result journal)  
**ADR:** [trash-and-paths](../decisions/trash-and-paths.md)  
**Last updated:** 2026-10-19

## Goal and behavior

```
facade (a later delete task) → platform.Trash(saveDir, names, platform.NewSystemTrasher())
  saveDir   the folder the torrent owns (validated by 009's Layout / 020)
  → checkFolder(saveDir)              refuse a volume root or the home (ErrUnsafeFolder)
  → os.OpenRoot(saveDir)             every name resolved inside the folder (hard confinement)
  → per name: OpenFile(O_NOFOLLOW)   refuse a symlink / escape; must be a regular file
            → Trasher.TrashFile(absPath)   NSFileManager trashItemAtURL (reversible, per file)
  → []Result (one per name) + ErrSomeRemain naming the files that stayed (DEL-04)
```

- Only the explicitly named files are passed to the Trasher; a neighbouring file in the folder is
  never touched (DEL-02).
- The **hard guarantee** is confinement: `os.OpenRoot` refuses a name that leaves `saveDir` through
  `..`, an absolute path or a symlink, so nothing outside the torrent's folder is ever trashed.
- A Trash failure, a missing file, a non-regular file or an escaping name leaves the file on disk and
  names it in `ErrSomeRemain`; nothing is ever deleted irreversibly (DEL-04). The file operation and
  any SQLite write are separate results — the two are not one transaction.
- Moving to the Trash is reversible; permanent deletion is never presented as it.

## Contracts

- `platform.Trasher interface { TrashFile(absPath string) (newLocation string, err error) }` — the
  one native operation. `platform.NewSystemTrasher()` is the real `NSFileManager` implementation
  (`trash_darwin.m`, cgo, `-framework Foundation`); tests inject a fake.
- `platform.Result{ Name string; Trashed bool; NewLocation string; Err error }` — one per requested
  name; a future delete coordinator (032) records the intent and this outcome.
- `platform.Trash(saveDir string, names []string, t Trasher) ([]Result, error)`.
- Errors: `ErrUnsafeFolder` (a volume root or the home), `ErrNotRegular` (not a regular file),
  `ErrSomeRemain` (at least one file stayed; it is named).

## Limits

- **TOCTOU residual:** `trashItemAtURL` re-resolves the path, so a regular file swapped for another
  regular file of the same name inside the folder between the check and the move cannot be told apart
  by a path-based API. Confinement bounds this to the torrent's own folder; 032 owns the journal.
- **`not_run`:** the full UI→Go→Trash flow has no delete UI yet; the real API is proven at the call
  level ([report 012](../verification/012-macos-trash-and-path-spike.md)).
