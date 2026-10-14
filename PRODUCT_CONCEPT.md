# OctopusTorrent: personal application concept

**Current decision:** 14 October 2026. The project is for personal use; the chosen stack is React + TypeScript, Wails, Go, anacrolix/torrent and SQLite.

## Idea

OctopusTorrent is a desktop BitTorrent client for macOS with a local React + TypeScript interface in Wails and a Go torrent service built on anacrolix/torrent. It should make it convenient to add torrents, choose files and the save location, show the transfer state and reliably continue working after a restart.

This is a single local version without accounts, Free/Pro plans, subscriptions, payments, license checks or a cloud service. The number of torrents is not artificially limited; all implemented capabilities are available right away. Product analytics and a public sales website are not needed.

## Chosen stack

| Part | Technology |
|---|---|
| Interface from the mockup | React + TypeScript in strict mode, Vite |
| Window and connection to Go | Wails v2, stable branch |
| Torrents, file operations, rules | Go |
| Torrent engine | anacrolix/torrent |
| Local library | SQLite through a Go repository |

Frontend assets are bundled into the `.app`; the UI runs in the system WebView, and all disk and engine actions are performed in Go. Our HTML mockup is ported into React components. The chosen stack does not create a requirement to ship the application for other platforms. Specific versions and the SQLite driver are pinned after a trial build. Wails v3 is not part of the current decision.

## Principles

1. **Data stays on the Mac.** The library, metadata, local paths, tags, rules and restore state are stored locally. No own server is needed. The internet is used for BitTorrent.
2. **Control before start.** Adding a `.torrent` opens a preview of the real files; for a magnet, the metadata is fetched first. The user chooses the folder, the files and whether to start immediately or paused.
3. **Files are kept by default.** Removing a torrent from the list stops the transfer but keeps the received files. Deleting files is a separate action with a list and confirmation.
4. **Clear states.** The status text, progress, direction icon and current speeds are consistent. Errors explain the consequence and the next action.
5. **Reliability before automation.** First we prove the engine, persistence and restore work; then we add library organization and rules.

## Core capabilities

- Adding a `.torrent` via a button, drag and drop and Finder; pasting and system opening of magnet links.
- A drop zone at the bottom of the sidebar that accepts one valid `.torrent`. The extension and contents are checked automatically.
- A file preview with nested folders, collapsing and whole-folder checkboxes. A partial selection has an indeterminate state.
- Choosing an existing folder or creating a new one by name inside the chosen parent folder.
- A list of any number of torrents and a details panel: progress, size, files, connections, download and upload.
- Start, pause, resume, seeding after completion and global speed limits.
- Persistent checkboxes for selecting multiple torrents. A panel for bulk start, stop and removal appears after selection; "Clear selection" resets it.
- Pause/play buttons with the actions "Do not download" / "Download" for individual files and whole folders in an already added torrent. If some files in a folder are skipped, its play selects all child files again. Bytes already written are not deleted; piece boundaries are taken into account when explaining the behavior.
- Restore of the library, manual pauses, file selection and engine data after a normal or crash restart.
- Local state filters and search by name, as in the mockup.
- Settings for the default folder, the theme "System" / "Light" / "Dark", the language and transfer.
- Six languages: English, Ukrainian, Spanish, German, French and Portuguese. File and torrent names are not translated.
- Registration of `.torrent` in macOS and instructions on how to make OctopusTorrent the default application.

## Library organization and automation

These capabilities remain part of the personal project and have no access restrictions.

**Tags and smart collections.** A torrent can have several tags. A collection is a saved local filter by tags and state; its contents change automatically. One torrent can belong to several collections without duplicating or moving files. Deleting a collection does not delete its torrents.

**Rules.** Based on the name or the add source, a rule assigns tags and a local save folder. The rule order and the result are visible before confirmation. A rule error or an unavailable folder does not start the download and does not change data silently.

**Watch folder.** The user explicitly chooses a folder on this Mac. A new `.torrent`, once it has finished being written, goes through metadata and duplicate checks. By default a preview opens; automatic adding must be enabled separately, and it creates the torrent paused. The source `.torrent` is not deleted automatically.

Server synchronization between Macs has been removed from the current scope. If it is needed for own devices, the exchange method will be decided separately.

## Global upload shutdown

The settings have "Stop upload for all torrents". The intent: active downloads continue, payload upload stops for current and new torrents, and completed entries do not start seeding. After the mode is turned off, the normal transfer policy returns while manual pauses are preserved.

The mockup demonstrates this state. The Go service uses a separate upload permission in anacrolix/torrent; the methods `DisallowDataUpload()` / `AllowDataUpload()` must not cancel manual pauses or wanted. For new entries, the policy is set before payload. A network prototype checks the effect on speed, protocol overhead traffic and already buffered data. Details are in `ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md`.

## Interface

The sidebar contains the application name, library sections and the drop zone. The top bar has "Add torrent", search and settings. Alerts are located under the top bar and do not cover the main actions.

The window keeps its size regardless of the number of torrents and expanded files. The list and details scroll independently; long names and paths are truncated with access to the full value. A path can be expanded and copied in full.

At the start of a card, ↓ shows a download, ↑ shows seeding and a pause sign shows a stopped transfer. A completed torrent without seeding also has a pause sign and the exact label "Completed". Download and seeding have the same badge color, completion is red, pause is neutral. Speeds are shown only during a transfer.

React/CSS reproduce the glass look of the navigation panels from the mockup in the system WebView; the content remains readable, and an opaque fallback is applied when needed. Keyboard, VoiceOver, system scaling, Reduce Transparency and Increase Contrast are supported.

## Architecture and development

**AI-first is the development and maintenance principle.** The owner specifies the desired behavior, and the code must be understandable to the AI that later reads, checks and changes it. This requires an explicit structure, unambiguous names, simple component boundaries, explanations of important decisions and reproducible checks. The repository stores all the necessary context so that a new agent can continue the work without the chat history and without the owner needing deep knowledge of the stack. The concrete criteria are AI-01–AI-10 in the requirements.

React shows its own snapshots and sends commands to Go through Wails bindings. TorrentService manages one anacrolix/torrent client; the command ordering preserves pauses and the upload policy. UUIDs, full v1/v2 infohashes, SQLite and metainfo/magnet restore the library; the storage engine stores piece completion separately. Recovery after a crash is verified on real files.

We keep the code in a Git repository. Go modules and the frontend lockfile pin dependencies, the Wails CLI builds the `.app`; the README describes Go, Node.js and the required macOS build tools. The personal version needs a working `.app` on the owner's own Mac; a website, a public installer and server components do not block development. Signing and the installation method are decided for this Mac. Checking third-party library licenses remains a technical requirement.

Documents: `PROJECT_REQUIREMENTS.md` holds the acceptance criteria, `APPLICATION_ARCHITECTURE.md` the local components and data flows, `ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md` the technical plan for the chosen engine. The HTML mockup demonstrates the interface and does not perform real BitTorrent transfer.

## Mandatory development method

Every change is made through **TDD: RED → GREEN → REFACTOR**. Before each green step is finished and integrated, all owned executable code must have **100% coverage** by the defined metrics. Lowering thresholds, hiding files or writing the required tests after the implementation is done is forbidden.

At the start of every session, the agent analyzes the architecture and the actual code, plans the simplest sufficient boundaries and reuse. The code is clean and idiomatic for Go/React/TypeScript/Wails/SQLite; shared rules are not copied. Refactoring is done along the way on green tests; technical debt is not carried into completed tasks.

The [docs/](docs/README.md) folder contains the engineering rules, documentation for each feature, decisions and evidence. The description and the implementation evolve in the same change. The exact rules are defined by [QA-01–QA-08](PROJECT_REQUIREMENTS.md) and the [engineering policy](docs/engineering/TDD_AND_QUALITY.md).

## Stages

1. A Wails + anacrolix/torrent prototype on the owner's own Mac: `.app`, import, transfer, pause, priorities, global upload, folders and restore.
2. The main React/TypeScript interface, Wails bindings, SQLite, file dialogs, safe deletion, alerts and settings.
3. Bulk actions, search, filters and large file trees.
4. Tags and collections, then rules and the watch folder.
5. Verification of the personal build on real scenarios and bug fixing.

A content catalog, TV streaming, a web version, a mobile application and cloud data copying are not in this scope.
