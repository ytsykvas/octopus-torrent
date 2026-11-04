# Data folder: where the library lives and who may open it

**Status:** `implemented`  
**Requirements:** `STACK-04`, `SET-02`  
**Related tasks:** [014](../../product/tasks/014-sqlite-schema-migrations.md); next [020](../../product/tasks/020-destination-filesystem-service.md), [028](../../product/tasks/028-application-startup-quit.md), [029](../../product/tasks/029-restore-recheck-reconciliation.md)  
**Last verified version:** branch `task/014-sqlite-schema`  
**Last updated:** 2026-11-04

## Goal and behavior

```
OctopusTorrent.app starts (main → run)
  → library.SQLiteVersion                       an app whose SQLite does not work does not start
  → resolveDataDir                               buildmode_app.go: datadir.FromEnvironment
      OCTOPUS_DATA_DIR set?  yes → that folder (absolute, clean)   no → ~/Library/Application Support/dev.tsykvas.octopustorrent
      MkdirAll 0700 → symlinks resolved → a folder of this user that its group and others cannot open
  → openLibrary(dir)                             library.OpenLibrary(dir/library.sqlite): created or migrated
  → wails.Run(window)                            … the owner uses the app, then quits
  → library Close                                the last thing before the exit
```

- The installed app keeps its data in `~/Library/Application Support/dev.tsykvas.octopustorrent`:
  today `library.sqlite` (schema 1, still empty); the marks store (`piece-completion.sqlite`, 018) and
  logs (017) follow.
- A start that cannot use the folder or the library stops before the window, with
  `OctopusTorrent: <step>: <reason>` on stderr and exit 1, and changes no row: an unusable folder,
  another application's SQLite file, a library of a newer version, a failed migration (the user-visible
  form is 028). Reading a refused file may still let SQLite checkpoint that file's own WAL or roll back
  its own hot journal, as any reader would; a file without them stays byte-identical.
- **Nothing but the owner's app opens the owner's folder.** The bindings generator (every `wails
  build`, `generate module`, `wails dev`) opens no folder at all; `wails dev` refuses to start without
  `OCTOPUS_DATA_DIR`; `workspace run`, the gate, `/qa` and every test pass their own folder.

## Architecture analysis before the change

- **Boundaries.** `internal/datadir` (standard library only) resolves and validates the folder;
  `internal/library` opens a path it is given; `main.go` puts them in order. The build mode is decided
  by three function-free files (`buildmode_app.go`, `buildmode_dev.go`, `buildmode_bindings.go`), so
  `main()` stays branch-free and `run` gets plain values.
- **Why before the window.** Wails runs `OnStartup` in a goroutine beside the window: opening there
  would show a window before refusing a library and race its quit. `wails.Run` returns after the
  window closed and `OnShutdown` ran (Wails v2.16.0 `internal/app/app_production.go:17-25`), so the
  library closes after everything that could use it.
- **Why tags.** Wails builds the bindings generator with `-tags bindings` and the dev app with
  `-tags dev`; at run time a branch build is indistinguishable from the installed app, so only the
  build mode can keep builds and `wails dev` out of the owner's folder.

## Contracts and data

| API | Contract |
|---|---|
| `datadir.Override` | `"OCTOPUS_DATA_DIR"` |
| `datadir.LibraryFile` | `"library.sqlite"`, the library's name in the folder |
| `datadir.Default(home)` | `<home>/Library/Application Support/dev.tsykvas.octopustorrent` |
| `datadir.FromEnvironment()` | the override when set, the default otherwise; returns the resolved folder, created `0700` with its parents when missing |
| `datadir.FromOverride()` | the override, required: the development build's resolver |
| `datadir.ErrUnusable` | the override is set but not an absolute, clean path (empty, relative, `..`, a trailing `/`, a NUL); the home folder is unknown or relative; the folder cannot be created (a file on the path), is not this user's, or has any permission bit for its group or others (so no file in it can be read by them, whatever its own mode) |

"Not inside a torrent save folder" is checked from the save folder's side: 020 refuses a save folder
that is, contains or lies inside the data folder; 029 checks the restored records.

## Scenarios and TDD

- `internal/datadir/datadir_test.go` (RED: `undefined: Override`): the default created `0700` under
  a temporary home and returned resolved; an existing folder kept with its files and mode; the override
  winning without looking up the home folder, created with its parents; a symlink resolved; every
  refused override creating nothing; a file, a symlink to a file, a path under a file, group- or
  world-writable, group- or world-readable and world-searchable folders and another owner (the `uid` parameter) refused; a home error or a relative
  home refused; `FromEnvironment` and `FromOverride` on the real process environment (`t.Setenv`,
  `HOME` in a temporary folder).
- `main_test.go` (RED: `undefined: system`): the order version → folder → library → window → close;
  each failure exits 1 with its line and runs nothing after it; a window failure still closes the
  library; a close error is printed and keeps exit 0; the bindings build calls neither folder nor
  library; `openLibrary` on a real folder, and on a folder whose `library.sqlite` is not a database
  (an error and a nil `io.Closer`, never a typed nil).
- The gate's `.app` run: the instrumented product, launched with
  `OCTOPUS_DATA_DIR=<root>/build/coverage/app-data/OctopusTorrent`, must leave `library.sqlite` and
  no `-wal`/`-shm` after the Apple Event quit.

## Implementation and reuse

| File | Responsibility |
|---|---|
| `internal/datadir/datadir.go` | `Override`, `LibraryFile`, `Default`, `FromEnvironment`, `FromOverride`, `ErrUnusable`; `resolve` and `choose` |
| `main.go` | `system` (the outside world of `run`), `run`, `openLibrary` |
| `buildmode_app.go`, `buildmode_dev.go`, `buildmode_bindings.go` | `generatingBindings` and `resolveDataDir` per build mode; no function, so nothing to measure |
| `internal/devtool/workspace.go` | `workspace run` → `OCTOPUS_DATA_DIR=<worktree>/build/workspace-data` |
| `internal/devtool/datafolders.go`, `appcov.go`, `check.go` | the gate's tool folder, `.app` data folders, `go vet` of every build mode and the "data folders" step ([quality gate](../tooling/quality-gate.md)) |

## Checks and 100% coverage

- `go test -race -count=1 ./internal/datadir`: `pass`, 100.0% of statements.
- `go test -race -count=1 .`: `pass`; `main()` is the only block left to the gate's `.app` run.
- `go vet ./...`, `go vet -tags dev ./...`, `go vet -tags bindings ./...`: `pass` (the gate runs all three).
- The gate run and `/qa`: in [report 014](../verification/014-sqlite-schema-migrations.md).

## Refactoring and debt check

`run` took four arguments; it now takes `system`, one struct of its outside world, and the start-test
split that 028's note asked for is done (`startedOptions`, the formatter's exact JSON in its own test).
`datadir.LibraryFile` lives with the folder's layout, so devtool checks the gate's folders without
linking SQLite. No ignore directive, no skipped test.

## Usage and recovery

- **Development:** `go tool devtool workspace run` (worktrees, with a port), or
  `OCTOPUS_DATA_DIR="$PWD/build/workspace-data" go tool wails dev`. Without the variable `wails dev`
  stops with `OctopusTorrent: find the data folder: …OCTOPUS_DATA_DIR is not set…`.
- **A manually launched branch build** (`wails build`, then Finder or `open`) is the application build
  and opens the owner's folder. Give it its own folder:
  `open -n --env OCTOPUS_DATA_DIR="$PWD/build/qa-data" build/bin/OctopusTorrent.app`. A branch with a
  new migration would otherwise upgrade the real library, and `main`'s build would then refuse it as
  newer (016 adds the backup that undoes it).
- **After switching a worktree to an older branch**, delete `build/workspace-data`: its library may be
  of a newer version.
- **An override** should sit inside a folder only you can write (as `build/…` in a worktree, or a
  `mktemp -d` folder): the check covers the data folder itself, not its parents.
- **A refusal** names its reason on stderr; the rows are unchanged. Fix the folder's owner or mode
  (`chmod 700 <folder>`), or move a foreign file away; never delete a `-wal` file, it holds commits.

## Change history and evidence

- 2026-11-04, task 014: the data folder, the start and quit order, build modes and the tooling's
  folders — [report 014](../verification/014-sqlite-schema-migrations.md), ADR
  [library-schema-and-data-folder](../decisions/library-schema-and-data-folder.md).
