# OctopusTorrent — anacrolix/torrent and Wails technical plan

**Status:** chosen stack, 14 October 2026. React + TypeScript / Wails v2 / Go / anacrolix/torrent / SQLite. This is a documentation review and integration plan; real transfer and restore by our application have not yet been prototyped.

Product behavior is defined by `PROJECT_REQUIREMENTS.md`, module boundaries by `APPLICATION_ARCHITECTURE.md`. The API below has been checked against the official pages and the open source files. The package documentation pages and master may describe different versions: before writing code we choose one module tag and re-verify the signatures against it. The old libtorrent research is kept in `archive/LIBTORRENT_TECHNICAL_RESEARCH.md` as a reference about the previous stack.

## 1. What the engine provides

anacrolix/torrent is a Go BitTorrent library with metainfo/magnet, DHT, PEX, uTP, web seeds and v2. It is used as a component of other projects. The library model is convenient for our Go service, but by itself it does not store product collections, rules or the owner's manual decisions. [Official repository](https://github.com/anacrolix/torrent).

The product needs its own adapter that translates our UUIDs and commands into library operations. The module version is pinned in `go.mod`/`go.sum`; third-party code is not copied into our service and is not reworked to fit the UI.

## 2. API map

| Need | Candidate API |
|---|---|
| Client | `NewClient`, `Client.Close` |
| Import | `AddTorrentSpec`, `TorrentSpecFromMagnetUri`, `TorrentSpecFromMetaInfoErr` |
| Metadata | `GotInfo`, `Info`, `Metainfo`, `Files` |
| Payload pause | `DisallowDataDownload`, `DisallowDataUpload` |
| Restoring permissions | `AllowDataDownload`, `AllowDataUpload` |
| File selection | `File.SetPriority`, `PiecePriorityNone`, previous priority |
| Progress | `File.BytesCompleted`, `File.Length`, `Stats` |
| Verification | `VerifyDataContext` |
| Removal from client | `Drop`, `Closed` |

The `DisallowDataUpload`/`DisallowDataDownload` fields of `AddTorrentOpts` (and so of a `TorrentSpec`) are never read on the pinned v1.61.0 ([engine-lifecycle](../docs/decisions/engine-lifecycle.md), finding 10): a new or restored record gets its prohibition from the `Torrent` method after the add and before `SetInfoBytes` ([global-upload-stop](../docs/decisions/global-upload-stop.md)). Before metadata is received, `Info` may be nil; that is not a ready file list. `MergeSpec` is not a way to replace storage. [API documentation](https://pkg.go.dev/github.com/anacrolix/torrent).

This is an integration map, not a promise that a single call implements the whole product scenario. The adapter controls ordering, the local record, cancellation, errors and the result.

## 3. Manual pause and global upload

We store `manualPaused`, `globalUploadDisabled`, file wanted and the actual error separately. The policy recomputes permissions after every command; pressing play does not restore upload if it is globally disabled.

In the current engine code, download and upload have separate flags; a download prohibition cancels requests. The upload implementation contains a TODO about updating active peer connections, so the mere existence of the method does not prove an immediate stop of all buffered payload. A controlled test must determine the confirmation boundary and measure the residual traffic. [torrent.go implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/torrent.go). Task 010 measured it: see [global-upload-stop](../docs/decisions/global-upload-stop.md) for the confirmed stop, the residual, and the two engine defects (a lost writer wake-up; no request after the last cancel is rejected) with their remedies.

Design decision: use per-torrent permissions for the runtime switch; new records get the corresponding prohibitions before their info is set (the initial spec flags are dead, see §2). Do not mutate the client configuration without a verified thread-safety contract. A separate `NoUpload` is a configuration-level disabling of sending chunks. `Seed` controls readiness to seed without reciprocity, and the upload limiter applies to chunks. [Client config](https://raw.githubusercontent.com/anacrolix/torrent/master/config.go).

Ordinary seeding after completion requires an explicitly chosen client seed policy and per-torrent permissions. A stopped record is not closed merely for the sake of a pause. Protocol messages, tracker/DHT or disk verification must not be presented as payload downloading.

## 4. Files, folders and pieces

`File.SetPriority` changes the priority of a file's pieces; `Download` uses normal priority, and the old `Cancel` is marked deprecated. Readers also affect piece demand, so for a simple client we do not add hidden readers to skipped files. [File implementation](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go).

Wanted belongs to our DB. Pausing a file sets None; play restores the saved priority. The folder button goes through all verified leaf indices; this is a service operation, not a folder object in the engine. Collapsing has no side effect. With partial wanted, the play button selects all descendants.

A download selection is not an upload permission for a specific file and does not delete bytes. For v1, boundary pieces between two files are verified separately; with None on one file, we cannot promise the absence of any bytes of its range if they are needed by the neighboring file. Completed files have exact progress regardless of wanted.

## 5. Metadata, paths and import

`.torrent` is parsed through metainfo before an active record is created. The metainfo API has Load/UnmarshalInfo, v1/v2 data, a file tree and piece layers. The full metainfo is stored without re-encoding info merely for the sake of the name; identity is not replaced by a short display name. [Metainfo API](https://pkg.go.dev/github.com/anacrolix/torrent/metainfo).

Our own validation sets limits on size, number of files and depth, and checks absolute paths, `..`, symlinks and collisions. Names are untrusted data. A frontend preview does not replace Go validation. We store the UUID and separate full v1/v2 identities; hybrid or later-received metadata may reveal a duplicate.

For a magnet, the pending import has a bounded, cancellable wait, a storage that holds nothing and payload prohibitions in both directions. A successful confirm attaches the final storage in place; no Drop or re-add is needed, so the UUID and the peer connections are kept. Cancel drops only the engine object of this pendingImportID. Verified in 011 ([magnet-pending-storage](../docs/decisions/magnet-pending-storage.md)): before confirmation no file is created, not even the empty ones, and no payload moves. A folder that already holds a path of the torrent is refused at the confirm. Open: the pinned engine applies a peer's metadata before it can be verified, and v2-only info under a v1 infohash panics it (finding 33, for 018/022).

Finder, URL, the button and the watch folder call one coordinator. Automatic adding is switched on explicitly and creates manualPaused. An unavailable folder or rule does not substitute a random destination.

## 6. Storage and restore

The Storage API has file backends, options for paths and piece-completion implementations, including SQLite. Some custom-path constructors are deprecated; the specific backend and its native dependencies are chosen on the pinned tag. Individual backends use different folder structures, so the library default is not the same as our save path. [Storage API](https://pkg.go.dev/github.com/anacrolix/torrent/storage).

Design plan: our own SQLite library with migrations, payload in the chosen folder, separate durable piece completion. At startup we add records with a payload prohibition, restore wanted/pause and only then allow transfer. Existing bytes and completion marks are verified according to the chosen policy; a full recheck may be needed after corruption or an external change.

We do not carry over to this stack the promise of a ready-made serialized fast-resume from another library. Behavior on normal close, crash, corrupted completion, moved files and storage change must be proven separately. Periodic saving of product settings is not deferred until quit.

## 7. Progress, speeds and errors

Our adapter aggregates completed bytes of only wanted real files for torrent progress; folder progress covers real descendants and shows the wanted count separately. Pad files are not included in product selection. If there are no wanted files, we show "No files selected"; a zero-length real file does not cause division by zero.

BytesCompleted is an estimate of the available completed/partially received data, not a speed counter. For rates we take the delta of payload counters over time; after restart/reset a new baseline sample is needed. Data verification may change the completed bytes. [File progress source](https://raw.githubusercontent.com/anacrolix/torrent/master/file.go), [Stats API](https://pkg.go.dev/github.com/anacrolix/torrent).

The UI hides rates for pause/done. ETA is computed only with a valid positive speed. We document whether bytes mean already written chunks or also pending ones; inspector and card data come from the same snapshot.

Go converts disk/network/metadata errors into our own AppError. A write-error callback does not automatically mean "torrent deleted"; its lifecycle is separate from manual pause. Error listeners, pollers and piece subscriptions have teardown. No business state is handed to React as a library pointer or mutable engine object.

## 8. Drop and file deletion

Library removal and product deletion are different operations. First we stop payload, remove the engine object and finish the associated tasks; then we remove the record. A separate confirmation allows the Go FileAccessService to work with a verified list of physical paths.

Shared files, symlinks, external changes and third-party content must not be deleted via a folder name. Use of the macOS Trash is verified separately; a partial result is written to the operation journal. SQLite and the filesystem are not one transaction. A crash in the middle of deletion must show an understandable state, not lose evidence of the remaining files.

## 9. Wails and frontend

Wails v2 provides desktop bindings and a runtime; its stable branch is chosen for the first version. The Vite frontend is included in the `.app`; all system operations remain in Go. Installation of development dependencies is described officially; we record the exact toolchain/native prerequisites after our own build. [Wails introduction](https://wails.io/docs/introduction/), [Installation](https://wails.io/docs/gettingstarted/installation/).

An initial snapshot is needed even with the emit/listener model: Go may start working before React mounts. Our own revision/operationID prevents the UI from being rolled back by a stale event. Browser component tests use a fake bridge; real dialogs, Finder/URL and quitting the `.app` are verified in Wails on a Mac. [Events](https://wails.io/docs/reference/runtime/events/), [Options](https://wails.io/docs/reference/options/).

React carries over the layout and tree of the mockup. WebView accessibility, focus after rerender, shortcuts, themes and the opaque fallback are part of the real criteria. We do not tie business logic to CSS or to WebView network-availability events.

## 10. Prototype plan and control scenarios

| Check | What to prove |
|---|---|
| Formats | `.torrent`, magnet, v1/v2/hybrid, multi-file, private, pad, zero-length, invalid metadata. |
| Import | Metadata-only cancel, two simultaneous sources, late duplicate, final folder and storage. |
| Transfer | Manual pause of both payload directions, resume, global upload, new record under the global policy, seeding after completion. |
| Files | None/normal, partial folder, collapsed descendants, shared piece, no wanted. |
| Restore | Close/relaunch, crash, completion corruption, modified/missing payload, manual pause before transfer. |
| File errors | Disk full, permission denied, symlink/collision, Trash failure, interrupted bulk delete. |
| Desktop | Cold Finder/magnet, repeated open, local assets, system dialogs, WebKit/VoiceOver, quit. |
| Concurrency | Metadata cancellation, stale events, goroutine/listener teardown, data races, slow SQLite operation. |

The prototype works with controlled test torrents and records the measured result. The existence of an API is not marked as a passed test. After it, we pin the SQLite driver, storage/transport choices, dependency versions and the exact build/check commands.

The MPL-2.0 license of anacrolix/torrent and the licenses of dependencies remain part of the dependency inventory; the personal project does not introduce a user licensing system. [LICENSE](https://github.com/anacrolix/torrent/blob/master/LICENSE).
