> **Archive of the previous stack — 14 October 2026.** This document describes libtorrent, which is no longer used in OctopusTorrent. It is kept only as a reference; the API, build and integration decisions here are not current requirements. The chosen stack: React + TypeScript, Wails v2, Go, anacrolix/torrent and SQLite. Current technical plan: [ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md](../ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md).

# Libtorrent defines the technical foundation of OctopusTorrent

**Conclusion.** For a native OctopusTorrent it makes sense to use **libtorrent-rasterbar 2.x as the C++ BitTorrent engine**, while SwiftUI remains the interface and product logic. The library already handles peer discovery, block exchange, hash verification, disk I/O, seeding and torrent resume; the application has to manage its session, turn events into understandable state and reliably persist its own list. The hardest parts specifically for OctopusTorrent are the **asynchronous Swift–C++ bridge**, honest semantics for selecting an individual file, persisting state after crashes, safe deletion and reconciling the local DB with fast-resume. The first technical stage must pin a specific libtorrent tag, prove that `.torrent` and magnet work on the owner's own Mac and verify local installation of the `.app`. Below is the documented behavior of the library, with the decisions we propose for the product marked separately. Research status: **10 October 2026**; the web reference `*-ref.html` shows version **2.1.0**, while the releases page contains newer patch releases, so exact signatures and behavior must be re-checked against the pinned tag before writing production code. ([Libtorrent manual](https://libtorrent.org/manual-ref.html)) ([libtorrent releases](https://github.com/arvidn/libtorrent/releases))

**Product scope updated on 14 October 2026:** a local application for personal use; technical primary sources were researched on 10 October 2026.

## The engine handles the protocol, but the product defines the user's library

### How data moves

`.torrent` is bencoded metadata: `info` describes the files, their sizes and piece hashes, but **does not contain the downloaded files**. The `info` hash identifies the torrent in the swarm. A client finds participants via a tracker, the DHT distributed table, PEX address exchange or LSD local discovery; then peers exchange information about the pieces they have, request blocks and transfer payload. Libtorrent manages TCP/uTP network connections, rarest-piece selection, integrity checks and seeding. By default, DHT, LSD, UPnP and NAT-PMP are available in the settings, but the specific policy for the application should be set and shown explicitly. **A tracker does not transfer file content**: it only helps find peers. A failure of one tracker also does not mean the torrent as a whole has no source. ([BEP 3](https://www.bittorrent.org/beps/bep_0003.html)) ([libtorrent features](https://libtorrent.org/features-ref.html)) ([libtorrent settings](https://libtorrent.org/reference-Settings.html))

In BitTorrent v1 the torrent hash uses SHA-1, and verification relies on piece hashes. **BitTorrent v2** uses SHA-256 and Merkle trees for files; a hybrid torrent can contain both forms and participate in the corresponding swarms. Therefore identity in our own database cannot be modeled by a single mandatory 20-byte hash. The 2.x API has `info_hash_t`, `v1()` and `v2()`; after metadata is received, a magnet record may acquire an additional hash and turn out to be a duplicate of another record. **Recommendation:** a local UUID is the stable record key in the UI, and v1/v2 hashes are separate optional fields for matching and local deduplication. Hybrid needs a test for identity conflict after `metadata_received_alert`. ([BEP 52](https://bittorrent.org/beps/bep_0052.html)) ([libtorrent migration to 2.0](https://libtorrent.org/upgrade_to_2.0-ref.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html))

`magnet:` usually contains an info-hash, an optional name and tracker URLs, but **not a file list**. `parse_magnet_uri()` turns the link into `add_torrent_params`; libtorrent obtains the metainfo from peers via the extension protocol. Before `metadata_received_alert`, `torrent_handle::torrent_file()` may be `nullptr`. So for magnet the interface must have a "Fetching file information" stage and must not draw an invented list or an exact total size. Metadata may never arrive at all if no peers are available; this is a separate waiting or error state, not an immediate "corrupted magnet". ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html))

**Boundary of responsibility.** Libtorrent performs the protocol and disk operations; OctopusTorrent defines collections, the watch folder, local rules, UI language and safe record deletion. The personal version has no artificial limit on the number of torrents. Validation, deduplication and creation of a `pending import` must be coordinated locally: concurrent drag-and-drop, Finder and watch folder must not create a duplicate. All entry points use one ImportCoordinator. This is a design decision consistent with `PRODUCT_CONCEPT.md`, `PROJECT_REQUIREMENTS.md` and `APPLICATION_ARCHITECTURE.md`.

### Short API map for the team

| OctopusTorrent need | Main libtorrent 2.x API | Important limitation |
|---|---|---|
| One engine for all torrents | `session`, `session_params`, `settings_pack` | Torrents are not part of the serialized session state. |
| Add `.torrent` | `load_torrent_file()` → `add_torrent_params` → `async_add_torrent()` | Parsing and validation before adding. |
| Add magnet | `parse_magnet_uri()` → `async_add_torrent()` | No files until `metadata_received_alert`. |
| Get state | `post_torrent_updates()` → `state_update_alert` | Torrents that changed are updated; the UI keeps the previous snapshot. |
| Pause and start | `torrent_handle::pause()` / `resume()` | For a strict manual pause, account for `auto_managed`. |
| File selection | `file_priority()` / `prioritize_files()` | Priority 0 is not an atomic "file pause". |
| File progress | `post_file_progress()` → `file_progress_alert` | Exact calculation is expensive; request moderately. |
| Local resume | `save_resume_data()` → alert → `write_resume_data()` | Separately for each torrent on this Mac. |
| Remove a record | `session::remove_torrent(handle)` | Pass `delete_files` only after separate confirmation. |

The signatures and names above come from the current 2.1.0 web reference; versions 1.2 and 2.x have API/ABI differences. **Before implementation, pin a specific tag and compile examples against exactly its headers**, rather than copying snippets from search. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Add torrent API](https://libtorrent.org/reference-Add_Torrent.html))

## The asynchronous session must give SwiftUI snapshots, not C++ objects

**Recommended architecture:** one long-lived `session` in `TorrentEngine`, a separate serial worker for commands and reading alerts, a narrow Objective-C++ or C API, a Swift service with immutable DTOs, and SwiftUI on the main actor. This is a recommended design, not a hard libtorrent requirement. Direct Swift/C++ interop is supported by modern Xcode, but for the first version a narrow adapter gives simpler control over C++ exceptions, `torrent_handle` lifetime, threads and Swift API stability. Public bridge methods must accept our own UUIDs and safe value types; C++ `session`, `torrent_handle`, `torrent_info`, pointers to alerts and `std::string_view` must not leak into the View. ([Apple: Swift and C++](https://developer.apple.com/videos/play/wwdc2023/10172/)) ([Session API](https://libtorrent.org/reference-Session.html))

`session` has its own network thread. Many `torrent_handle` methods that **return values** may wait for that thread's response; do not call them on the SwiftUI main actor. `async_add_torrent()` returns quickly, and `add_torrent_alert` delivers the result. `session::post_torrent_updates()` produces `state_update_alert` for changed torrents. The `set_alert_notify()` callback must only wake our worker; `pop_alerts()` is called there. The received pointers belong to the session and are valid **only until the next `pop_alerts()`**, so the worker immediately copies the needed fields into its own DTOs. The alert queue is bounded, and `alerts_dropped_alert` means a full snapshot must be restored and the polling frequency/queue size adjusted. ([Session API](https://libtorrent.org/reference-Session.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

**Lifecycle sketch, not compilable without checking against the chosen tag:**

```text
startup:
  read the local DB and the verified local session state
  create session from session_params/settings_pack
  for each record, restore its own resume or metainfo
  async_add_torrent(params); match add_torrent_alert to the local UUID

import .torrent:
  load_torrent_file(path, limits); validate metadata and paths
  show the files/folder dialog; check duplicates; apply file priorities
  async_add_torrent(params); on add_torrent_alert update DB/UI

import magnet:
  parse_magnet_uri(uri); create a controlled metadata-only record
  wait for metadata_received_alert; show the actual file list
  after confirmation set folder/priorities and allow payload

event loop:
  pop_alerts(); immediately copy the data of each alert
  handle add/metadata/state/file/error/resume/remove events
  periodically post_torrent_updates(); separately request file progress for the visible UI
  publish an aggregated snapshot on the main actor

shutdown:
  asynchronously save the dirty resume of each torrent; wait for results
  atomically write state files and a separate session_params
  end the session without arbitrarily changing paused flags
```

For obtaining only magnet metadata, the documentation describes `torrent_flags::upload_mode`: it does not request payload, **but the engine may create empty files**. Therefore the preview dialog should be prototyped and tested on APFS; do not promise that canceling a magnet never wrote anything. After `metadata_received_alert`, the received info dictionary must be saved so that the next launch does not wait for the same metadata. ([libtorrent manual](https://libtorrent.org/manual-ref.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html))

**UI state is derived, not the raw `torrent_status::state`.** Libtorrent distinguishes `checking_files`, `downloading_metadata`, `downloading`, `finished`, `seeding`, `checking_resume_data`; pause is a flag, and a disk error is available via `errc`/alert. OctopusTorrent first checks for an error, then manual/automatic pause, checking, waiting for metadata, seeding, finished and downloading. `finished` may mean only the **selected** files are complete, whereas `seeding` means the full set of pieces. The "Files ready" and "Seeding" statuses should not be equated. For a torrent-wide manual pause, `auto_managed` must be disabled, or the queue policy designed so that it does not start writing without a user command; this is especially important for automatic adding from the watch folder. ([Torrent status API](https://libtorrent.org/reference-Torrent_Status.html)) ([Core flags](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

| Event | Meaning for the application |
|---|---|
| `add_torrent_alert` | Confirm the addition or show the reason for refusal; finally bind the handle and UUID. |
| `metadata_received_alert` / `metadata_failed_alert` | Build the file list or show a metadata error, which may be temporary. |
| `state_update_alert`, `state_changed_alert`, `torrent_finished_alert` | Update the list, inspector, progress and status transitions. |
| `file_prio_alert`, `file_progress_alert` | Confirm file selection and update their progress. |
| `file_error_alert`, `torrent_error_alert`, `tracker_error_alert` | Distinguish a torrent disk failure from a single tracker failure; localize the message. |
| `save_resume_data_alert` / `save_resume_data_failed_alert` | Complete or retry saving the local state. |
| `torrent_removed_alert`, `torrent_deleted_alert`, `torrent_delete_failed_alert` | Separate record removal from the actual result of file deletion. |
| `alerts_dropped_alert`, `listen_failed_alert` | Restore the current state or show network diagnostics. |

Alert categories are set by `alert_mask`; one cannot assume that the default mask provides the full UI telemetry stream needed. The library does not localize `error_code::message()` messages for OctopusTorrent's six languages; the application must map the category and numeric code to its own texts, keeping the technical code in diagnostics. ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Manual: translating errors](https://libtorrent.org/manual-ref.html))

## Files, progress and resume need more precise semantics than the mockup

### Import, file selection and folders

`load_torrent_file()` allows parsing a `.torrent` **before adding it to the session**. For drag-and-drop, the `.torrent` extension is only a quick filter; the final check is a successful parse with `load_torrent_limits`, checking the number of files, directory depth, metadata size, symlinks, duplicate and unsafe paths, and collisions in the target folder. `torrent_info::layout()` and `file_storage` provide the list and sizes of files. The dialog must show the folder, the total size of **real, non-pad files**, file selection and future paths. Folder selection/creation, security-scoped bookmarks under App Sandbox and the system `.torrent` association are at the macOS level, not libtorrent. ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

Controlling the download of an individual file is not a pause of the whole torrent and has no API of that name. For an individual file, libtorrent uses `file_priority(index, download_priority_t::dont_download)`; priority 0 means **"do not download the selected file any further"**, not instantly stopping every related network block. In v1 one piece can cross the boundary of two files; the engine may fetch the boundary bytes. Priority changes are asynchronous and confirmed by `file_prio_alert`. Already written data does not disappear; in the seed state the priority may not affect current seeding, and file control is unavailable before metadata appears. **UI decision:** pause/play buttons with "Don't download" / "Download" actions and a "Skipped" state, with an explanation about bytes already received. This is selection of files to download, not an exact instant pause. Returning to download restores the saved previous priority. Do not mix file priorities with piece priorities without a plan: changing the former recalculates the latter. ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

### Folder tree on top of libtorrent's flat list

`file_storage` provides files with relative paths and stable indexes, not separate folder objects. The UI builds the tree from path components, keeping each leaf's index for `file_priority()` and progress. We do not duplicate the torrent's root name in the tree. Before displaying and writing, we check absolute paths, `..`, symlinks and collisions; pad files are hidden from selection and product totals. Collapsing a folder is UI state only. ([Storage API](https://libtorrent.org/reference-Storage.html))

In an already added torrent, a folder's play/pause button changes the selection of **all child files**, including completed ones and files in nested folders. The client stores each leaf's selection and applies it via `file_priority()` by index; existing bytes are not deleted, and excluding a completed file does not guarantee seeding stops instantly. libtorrent has no separate "folder pause". When all child files are selected, pause excludes them; with zero or partial selection, play is shown, which selects everything. In the dialog before adding, a checkbox with an indeterminate state remains for this. Folder progress is the sum of available bytes / the sum of sizes of visible real descendants, and the number of selected files is counted separately; both totals do not depend on collapsing. For large trees, build nodes once from the metainfo, update only visible rows and check performance on thousands of files. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

### One formula for progress

For the download card of selected files, the most consistent pair is `total_wanted_done / total_wanted`. `total_payload_download` is a counter of transferred bytes, not the number of useful bytes currently present; `total_size()` may include pad files. `download_payload_rate` and `upload_payload_rate` show payload bytes per second; `download_rate`/`upload_rate` also include protocol overhead traffic. The percentage and "4.2 GB of 5.9 GB" must be computed **from one pair of fields and rounded the same way**, otherwise the mockup will show contradictory figures. If all files are skipped and `total_wanted = 0`, show "No files selected", not 100%. ETA is our own approximate estimate `(total_wanted − total_wanted_done) / payload_rate`; at zero speed, while paused or while checking, it is better hidden. ([Torrent status API](https://libtorrent.org/reference-Torrent_Status.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html))

For each file there is `post_file_progress()` and `file_progress_alert`. Exact `file_progress()` is relatively expensive; the `piece_granularity` mode is cheaper and counts only fully verified pieces. **Recommendation:** request data for the visible expanded torrent and the inspector, after `file_prio_alert` or at a measured interval; not for all files of all torrents on every UI frame. An initial 1–2 list updates per second is only a hypothesis to measure, not a libtorrent guarantee. Separately measure memory, CPU and response time with hundreds and thousands of files. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Session API](https://libtorrent.org/reference-Session.html))

### Two different persistence layers

`session::session_state()`/`write_session_params_buf()` save global parameters, including DHT/settings, **but not the list of torrents**. Each torrent has its own fast-resume: `save_resume_data()` asynchronously returns `add_torrent_params` via an alert, which can be serialized with `write_resume_data()`; at startup it is read with `read_resume_data()`. Resume contains local information about paths, received pieces, paused state and file priorities. The synchronous `get_resume_data()` should not be called from the UI thread. For magnet, the info dictionary should be saved once metadata appears. With an invalid resume, `fastresume_rejected_alert` and a full file check are possible. **Recommendation:** write resume atomically, keeping a previous copy or using a transactional replacement; track dirty state and save after changes, on pause/completion and on shutdown, but do not defer everything until the application closes. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Resume data API](https://libtorrent.org/reference-Resume_Data.html)) ([Manual: fast resume](https://libtorrent.org/manual-ref.html))

**Resume belongs to a specific Mac and specific files.** It contains the local `save_path`, pieces, priorities and pause; the documentation requires protecting the resume file from untrusted modification. The personal version stores it locally. The DB holds the UUID and full v1 SHA-1/v2 SHA-256, metainfo or magnet, and local tags/rules; a truncated v2 hash is not a universal key. If the files or the path have changed, a check and an explicit user decision are needed, not trust in stale resume data. ([Resume data API](https://libtorrent.org/reference-Resume_Data.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

### Deletion and moving

`session::remove_torrent(handle)` without special flags removes the record from the session, leaving the files. `delete_files` deletes the torrent's files together with the part-file, `delete_partfile` only the part-file. For OctopusTorrent the default action must be **"Remove from list, keep files"**. A separate "Delete torrent and files" requires confirmation with a list of paths and an explicit `delete_files`; the result of physical deletion must be tracked via `torrent_deleted_alert` or `torrent_delete_failed_alert`, because removal from the session and deletion from disk are different outcomes. The same requirement applies to bulk delete for each selected torrent. After remove, do not use the old handle: it becomes invalid. ([Session API](https://libtorrent.org/reference-Session.html)) ([Alerts API](https://libtorrent.org/reference-Alerts.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

Before `delete_files`, it is worth additionally detecting shared physical files, foreign files in the folder, renamed or absolute paths and symlinks: the API does not formulate a product guarantee of "only the shown files will be deleted" for these cases. If the exact list cannot be proven, it is safer to offer keeping the payload or require a separate check. A normal remove without `delete_partfile` may leave a technical part-file; its cleanup policy must be explicit and tested, especially when the user expects to keep the ability to quickly resume the torrent. ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

`move_storage()` to another disk may copy and delete the old files; the result is asynchronous, and the default mode may overwrite the target. `rename_file()` is also asynchronous, and `torrent_info::layout()` stays the original after renaming. Therefore, when changing the folder of an already active torrent, a separate operation with progress/error must be shown, the target folder checked and the replacement mode chosen carefully. Setting the **default folder for future downloads** must not silently move already existing files. ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

For the first prototype, the recommended `storage_mode_sparse` and the standard disk I/O backend are sufficient. A different backend or full allocation makes sense only after measuring APFS, an external disk and concurrent load; libtorrent does not provide a ready optimal profile for OctopusTorrent. Moving between volumes should be tested for interruption and lack of space, because copying can delay other disk operations. ([Manual: storage allocation](https://libtorrent.org/manual-ref.html)) ([Storage API](https://libtorrent.org/reference-Storage.html))

### Global no-upload mode needs a separate prototype

The user's request is to **stop transferring payload pieces for all torrents**, while keeping active downloads. This is not the same as pausing only completed torrents: while downloading, a client also uploads pieces already received. A plain `settings_pack::upload_rate_limit = 0` does not work: **0 means unlimited**, and a very small limit may also throttle the protocol messages needed for downloading. `active_seeds = 0` controls the queue of completed torrents but does not prove zero upload from active downloads. `torrent_flags::upload_mode` means the opposite — payload downloading stops. ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

A spike on a pinned libtorrent version is needed: check unchoke control and peer classes or a narrow engine extension, separate payload from protocol traffic, measure `upload_payload_rate` and bytes sent in a controlled swarm, and confirm that download continues. `unchoke_slots_limit` cannot be treated as a universal guarantee: behavior depends on the choking algorithm. Test normal and private trackers, local peers, new and already active torrents, restart and turning the mode off. If exact behavior is unattainable, the product must show an honest alternative — an upload rate limit or automatically stopping seeding after completion — without the name "no upload". ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Manual: choking](https://libtorrent.org/manual-ref.html))

## Network, security and distribution shape the boundaries of the first release

Libtorrent opens an incoming port and with default settings uses DHT/LSD mechanisms and port mapping; LSD may announce the info-hash on the local network. Proxy switches for peer connections, tracker, DNS and DHT cover different streams. `anonymous_mode` only partially changes the client's visible identifiers; protocol encryption **is not equal to a VPN or a guarantee of anonymity**. Private torrents should be tested with DHT/PEX/LSD according to their private flag and tracker rules. **Recommendation:** in the first release, have documented actual defaults, port and tracker diagnostics, and for a future proxy/VPN-only mode separately test DNS, UDP, switching between networks and fail-closed behavior. ([Settings API](https://libtorrent.org/reference-Settings.html)) ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Mozilla security assessment of libtorrent](https://libtorrent.org/2020%20Q4%20Mozilla%20Libtorrent%20Report%20Public%20Report.pdf))

Incoming `.torrent` and magnet are untrusted data. The parser should be given limits (`load_torrent_limits`, `max_metadata_size`), and our own file actions should check normalized paths, symlinks, collisions and sufficient space. A watch folder rule must react only to a completed `.torrent` write, check the content with the parser and for duplicates, and not delete the source file after a failure. **Recommended default:** show the preview dialog by default; auto-add, if the user has separately enabled it, creates a paused record and goes through the same validation and deduplication. Smart collections are filters over the local DB, not duplication of torrents in the `session`. ([Torrent info API](https://libtorrent.org/reference-Torrent_Info.html)) ([Core API](https://libtorrent.org/reference-Core.html)) ([Settings API](https://libtorrent.org/reference-Settings.html))

For the personal build, pin the libtorrent tag/commit and the Boost/TLS dependency versions. The official build supports CMake and Boost.Build; CMake requires C++17, and the configuration and `TORRENT_*`/`BOOST_*` definitions must be consistent between the library build and the application build. Build for the owner's own Mac architecture; each dynamic dependency must match it and be placed in the proper location in the `.app`. A universal binary is needed only if both architectures must be supported. Static linking is a candidate for the first spike, but the choice should be made after an actual build, license analysis and artifact size. Do not require Homebrew from the end user. ([Libtorrent building](https://libtorrent.org/building.html)) ([CMake: macOS architectures](https://cmake.org/cmake/help/latest/variable/CMAKE_OSX_ARCHITECTURES.html)) ([Apple: bundle content](https://developer.apple.com/documentation/bundleresources/placing-content-in-a-bundle))

We verify the personal build locally on the target Mac first. Future public distribution needs a separate pipeline: Developer ID, Hardened Runtime, secure timestamp, signing nested code from internal libraries outward, notarization and stapling. App Sandbox **does not follow automatically** from `.dmg`/Developer ID; enabling it is a separate decision. If enabled, the entitlements for incoming and outgoing network, access to the download folder and watch folder, and long-lived security-scoped bookmarks must be checked. Verify the personal build on the target Mac without depending on local Boost/OpenSSL; for public distribution verify all supported architectures, with `codesign --verify`, `spctl`, the notarization log and `stapler validate`, as well as real import, download, seeding and resume. ([Apple: notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution)) ([Apple: Hardened Runtime](https://developer.apple.com/documentation/security/hardened-runtime)) ([Apple: App Sandbox](https://developer.apple.com/documentation/security/app-sandbox))

The license terms of libtorrent and its dependencies remain a requirement for building and distributing third-party code. The binary package must preserve the copyright, terms and disclaimer. `LICENSE` also contains texts for included code fragments; Boost, OpenSSL/GnuTLS and the submodules actually built need a **separate license audit list**. The .app/.dmg should include `Third-Party Notices` and a "Licenses" screen. This is a technical checklist, not a legal opinion: the final notice package must be checked against the actual lockfile and linked objects. ([Libtorrent LICENSE](https://github.com/arvidn/libtorrent/blob/RC_2_1/LICENSE)) ([Libtorrent CMakeLists](https://github.com/arvidn/libtorrent/blob/RC_2_1/CMakeLists.txt))

## The prototype must prove the risky scenarios before developing the whole interface

Implementation order for personal use. **First spike:** choose a 2.0.x or 2.1.x tag based on compiling on the owner's own Mac, pin the SHA, build for its architecture, make a minimal bridge, add a test `.torrent` and magnet, receive alerts, perform pause/resume, file priority, save/restore resume and remove a record while keeping the payload. Separately prove global stopping of upload during an active download. **Second stage:** local DB, stable UUID, aggregated snapshots, import dialogs, correct statuses, alerts and settings. **Third:** bulk actions, search, filters, watch folder, rules and collections. Checking other architectures and notarization is needed when there is a separate need to distribute the build. ([Libtorrent building](https://libtorrent.org/building.html)) ([Session API](https://libtorrent.org/reference-Session.html)) ([Torrent handle API](https://libtorrent.org/reference-Torrent_Handle.html))

| Verification matrix | What must be proven |
|---|---|
| Formats | v1, v2, hybrid; `.torrent`, magnet v1/v2, private torrent, multi-file, pad and zero-size files. |
| Files | Select/skip, boundary piece between files, priority change during transfer, progress and status after skipping all files. |
| State | Restart during download/seed, crash while writing resume, corrupted resume, moved or modified payload, unavailable folder. |
| Network | No internet, unavailable tracker, DHT-only torrent, Wi-Fi change, Mac sleep/wake, busy listen port, proxy/VPN if they appear. |
| Errors | Full disk, permission denied, invalid metainfo, lack of peers for magnet, alerts overflow, failure to delete files. |
| Product | Multiple torrents are added without an artificial limit; watch folder goes through the same metadata and duplicate check; auto-add creates a paused record; bulk delete keeps files without a separate choice. |
| Scale | Measure startup time, RAM, CPU, alerts/sec, UI latency, disk I/O and battery for libraries of 100/1000 torrents and large file lists. |

Important decisions that libtorrent **will not make for us** must be recorded before production code: minimum macOS; 2.0.x or 2.1.x tag and the update scheme; C ABI or Objective-C++ bridge; App Sandbox; exact metadata limits and symlink policy; local resume retention period; UX of magnet metadata-only and "file skip"; canonical v1/v2/hybrid key for local deduplication; transactional persistence of tags and collections; error localization; the need for and verified scope of proxy/VPN features. Performance figures and snapshot frequency should not be announced in advance — they will be set by a spike on the weakest supported Mac.

## Conclusion

Libtorrent reduces the amount of work with the BitTorrent protocol, but moves the main engineering risk to the **boundary between the asynchronous C++ engine and the durable local state of the macOS application**. For the personal version, one isolated engine, our own DB, alerts as the source of changes and local fast-resume are sufficient. Collections, rules and the watch folder work on top of this library without a server layer.

### Short glossary

**Peer** — a swarm participant that downloads and/or uploads blocks. **Seed** — a peer that has the entire required data set. **Tracker** — a peer discovery service. **DHT** — distributed peer discovery without a central tracker. **PEX** — exchange of peer addresses between participants. **LSD** — peer discovery on the local network. **Piece** — a part of torrent data whose integrity is verified by a hash; **block** — a smaller unit of request to a peer. **Info-hash** — the hash of the `info` metadata, the torrent's identifier in the swarm. **Fast-resume** — local state for quickly continuing after a restart, not a way to transfer payload between Macs. ([BEP 3](https://www.bittorrent.org/beps/bep_0003.html)) ([Libtorrent manual](https://libtorrent.org/manual-ref.html))

### Main primary sources

| Topic | Source |
|---|---|
| Architecture, lifecycle, examples | [Manual](https://libtorrent.org/manual-ref.html), [Tutorial](https://libtorrent.org/tutorial-ref.html), [Internals](https://libtorrent.org/hacking.html) |
| Main APIs | [Session](https://libtorrent.org/reference-Session.html), [Torrent handle](https://libtorrent.org/reference-Torrent_Handle.html), [Torrent status](https://libtorrent.org/reference-Torrent_Status.html), [Alerts](https://libtorrent.org/reference-Alerts.html) |
| Metadata and files | [Core](https://libtorrent.org/reference-Core.html), [Torrent info](https://libtorrent.org/reference-Torrent_Info.html), [Storage](https://libtorrent.org/reference-Storage.html), [Resume data](https://libtorrent.org/reference-Resume_Data.html) |
| Protocol and versions | [BEP 3](https://www.bittorrent.org/beps/bep_0003.html), [BEP 52](https://bittorrent.org/beps/bep_0052.html), [Upgrade to 2.0](https://libtorrent.org/upgrade_to_2.0-ref.html) |
| Build and release | [Building](https://libtorrent.org/building.html), [Releases](https://github.com/arvidn/libtorrent/releases), [LICENSE](https://github.com/arvidn/libtorrent/blob/RC_2_1/LICENSE), [Apple notarization](https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution) |
