# OctopusTorrent — project requirements

**Status:** working specification v0.6 — personal use (14 October 2026)  
**Platform:** macOS  
**Technologies:** React + TypeScript · Wails v2 · Go · anacrolix/torrent · SQLite  
**Code principle:** AI-first — code and documentation are designed to be easy for AI to read, verify and change later.  
**Related materials:** `PRODUCT_CONCEPT.md`, `APPLICATION_ARCHITECTURE.md`, `ANACROLIX_TORRENT_TECHNICAL_RESEARCH.md`, `MOCKUP.html`

## 1. Goal and boundaries of the personal version

OctopusTorrent is a desktop BitTorrent client for personal use on the owner's Mac. The goal is convenient downloading and seeding, file selection, library management and reliable restore after restart. It is a single local version without accounts, paid plans, subscriptions, payments, a license server or cloud sync.

In this document **MVP** means the first working personal version. Collections, rules and the watch folder remain agreed capabilities and are implemented after the core engine has been verified. Details marked **"to be determined"** are clarified based on prototype results and the characteristics of the owner's Mac.

### Application rules

1. The number of torrents in the list has no artificial limit. Real limits are set by memory, disk, network and queue settings.
2. All implemented features are available immediately, without sign-in or access checks.
3. The list, metadata, settings and restore state are stored on this Mac.
4. Removing an entry stops the transfer and by default leaves the downloaded files on disk. Deleting files requires a separate choice and confirmation.

### Chosen stack

| Part | Technology | Responsibility boundary |
|---|---|---|
| Interface | React + TypeScript in strict mode, Vite | Layout, interactions, accessibility, localization and presentation state. |
| Desktop shell | Wails v2, stable branch | macOS window, system WebView, bindings, menus, dialogs and open events. |
| Logic | Go | Torrents, file operations, rules, library and lifecycle control. |
| BitTorrent | github.com/anacrolix/torrent | Network protocol, peers, pieces, metadata and integrity checks. |
| Local library | SQLite via Go | Transactions, schemas and migrations of the application's own data. The specific driver is fixed after the prototype. |

The application stays local, for personal use on macOS. The first target is the owner's Apple Silicon Mac; the portability of the stack does not create a requirement to ship Windows/Linux versions. A separate website or remote backend is not needed. Moving to Wails v3 is considered separately after a stable release and a compatibility check.

## 2. MVP feature requirements

### 2.1. Adding a torrent

| ID | Requirement | Acceptance criterion |
|---|---|---|
| ADD-01 | Add a local `.torrent` file via a button in the application, the "Open With" command, and a double-click in Finder if the user has made OctopusTorrent the default application. | After a valid file is opened, the application launches or activates and shows the name, contents and transfer settings before adding; an invalid file produces a clear error. The same scenario applies when the application is launched from Finder and when it is already open. |
| ADD-02 | Add magnet links by pasting them in the application and by opening the link through the system. | The link is accepted, metadata is downloaded, and the user sees a waiting state; an invalid link does not create a broken entry. |
| ADD-03 | Do not create a duplicate of an already added torrent. | Opening the same torrent again navigates to the existing entry and does not create a second entry. |
| ADD-04 | Before start, allow choosing the destination folder and the files to download. | For `.torrent` the choice is available immediately; for magnet, after metadata is received. The choice is preserved after restart. |
| ADD-06 | Accept a single `.torrent` by drag-and-drop onto the zone at the bottom of the sidebar. | While hovering, the zone is highlighted; if the file type is already known to be unacceptable, the zone shows a rejection. After drop, the file count, extension and metadata contents are checked. Only a valid `.torrent` opens the preview; other files are not added and produce a clear error. Clicking the zone opens the system file picker. |
| ADD-07 | Show a torrent settings dialog before adding. | The dialog shows the real file list from the metadata, their sizes, selection of individual files and of all at once, the total size of the selection, the save folder and a toggle to start after adding. Adding is unavailable with no files selected. Cancel adds nothing; confirm saves the chosen settings and adds the torrent. |
| ADD-08 | In the add dialog, allow choosing an existing folder or creating a new one by giving its name and parent folder. | The new folder is created on disk only after explicit confirmation. The application checks the name, write permission and file system errors; if a folder with that name already exists, it does not silently overwrite it. On success the new folder becomes the save location of this torrent. |
| ADD-09 | Show nested folders in the file preview. | Files from the `.torrent` are displayed as a tree by relative paths, without duplicating the torrent's root name. Folders can be collapsed, and all child files can be selected with one action; a partial selection has an indeterminate state. The size total is computed from the selected files regardless of whether a folder is collapsed. The full relative path is available in a tooltip and to VoiceOver. |

### 2.2. Downloading and seeding

| ID | Requirement | Acceptance criterion |
|---|---|---|
| TR-01 | Start, pause and resume a torrent. | A manual pause forbids payload download and upload; resume allows download and upload only under the current global policy. The pause is stored in SQLite across restarts. Control traffic and file checks are not called active payload transfer. |
| TR-02 | Download the selected files and show progress in real time. | The list and details show status, percentage, downloaded amount, current speeds and the estimated time to completion if it can be computed. |
| TR-03 | Keep seeding after completion. | A completed torrent may seed until the user pauses it, removes it from the list or turns on the global upload stop under TR-08. |
| TR-04 | Provide global settings for maximum download and upload speed. | The user can disable the limit or set a value; the setting is preserved after restart. |
| TR-05 | Resume work after a network loss or an application restart. | The application does not re-download existing valid data and does not change the selected files or folder without a user action. |
| TR-06 | Allow stopping further targeted download of an individual file and selecting it again. | The "Don't download" / "Download" actions change wanted in SQLite and call `File.SetPriority(PiecePriorityNone)` / restore the previous priority in anacrolix/torrent. This is not a pause of the whole torrent and not a deletion of bytes. A shared piece may require boundary bytes for other selected files. Excluding a completed file does not guarantee that its seeding stops. File controls are unavailable until magnet metadata is received. |
| TR-07 | Allow download control for a whole folder. | In an already added torrent, the pause button excludes all files of the folder from further download, including completed ones, nested ones and those hidden in collapsed folders. If at least one child file is excluded, the folder shows play: clicking it selects all files again. The count of selected files shows the partial state. The action does not delete bytes, does not pause the whole torrent and is not a separate folder object or folder priority in the engine. Before adding, the preview checkboxes remain the way of selecting under ADD-09. |
| TR-08 | Control upload globally, independently of download. | "Stop upload for all torrents" applies to current and new entries. The Go service forbids payload upload without disabling active download; completed torrents do not seed. Turning the mode off preserves manual pauses. For current entries `DisallowDataUpload()` / `AllowDataUpload()` are used, for new ones the initial TorrentSpec flags. The behavior, including already buffered data, is verified in a controlled swarm on the pinned engine version. |

### 2.3. List and details

| ID | Requirement | Acceptance criterion |
|---|---|---|
| UI-01 | Show the torrent list with name, status, progress, size and speed. | The user can tell apart downloading, paused, completed, seeding, error and waiting for metadata. |
| UI-02 | Show the details of the selected torrent. | Name, files, folder, downloaded amount, download and upload speed, number of connections, state and main actions are available. |
| UI-03 | Explain the empty state and errors in plain language. | The empty list offers to add a torrent; a network error, folder access error or lack of space has a description and a next action. |
| UI-04 | Implement the interface in React + TypeScript in Wails. | The application's own `.app` window contains a local React UI in the system WebView; menus, file dialogs and system events go through Wails/Go. The main actions are available from the keyboard; semantic HTML elements, focus management, VoiceOver, UI scaling and both themes are verified in the real WebView on a Mac. |
| UI-06 | Keep the glass styling of the sidebar and top bar from the mockup. | React/CSS reproduce the colors, transparency and spacing of the mockup; supported Wails window options are applied separately. Torrent content has a readable opaque background. With reduced transparency a solid style is used. Appearance, contrast and scaling are verified in the system WebView on a Mac. |
| UI-07 | Allow selecting several torrents in the "All Torrents" section and running a bulk action. | Checkboxes next to torrents are always available. After at least one torrent is selected, a bar appears with the selected count, "Select All", "Clear Selection" and the actions "Start", "Stop", "Remove". There are no buttons to enter or leave a separate selection mode. Clicking the card itself opens its details and does not change the bulk selection. Search does not lead to an action on hidden entries. |
| UI-08 | Show state and speed directly in the torrent card. | Under the progress bar, on one line, the status badge, the downloaded amount and, only during active transfer, the current speed are visible. "Downloading" and "Seeding" have the same badge color but different arrow directions: ↓ for download and ↑ for seeding. "Completed" has a red badge, "Paused" a neutral one; for both, speed is hidden in the card and details rather than shown as a dash. The text state stays visible regardless of color. |
| UI-09 | Show play/pause buttons next to each file in the card and details. | A selected file shows pause with the "Don't download" action, an excluded file shows play with the "Download" action. This also applies to completed files; the completion checkmark does not replace the control button. Progress and text separately show completion or skipping. Both places update consistently; the button is keyboard accessible, has a VoiceOver name and keeps focus after being pressed. The behavior matches TR-06. |
| UI-10 | Show errors and lack of network in a separate area under the window's top bar. | There is no permanent "connection active" indicator in the sidebar. If the network is gone, the alert explains the consequences for network transfer and offers to retry the check. It does not cover the list, has a close button, is keyboard accessible and is announced by VoiceOver. Errors of an individual torrent are also marked next to its entry and in the details; successful actions remain short notifications. |
| UI-11 | Expand the torrent card and also show files in the details panel. | A separate expand button opens the list of all files of the torrent; next to each, the name, downloaded and total size, percentage and its own progress bar are visible. The size sits above the progress bar on one line with the name and percentage. The details panel shows the same current file state. Long names do not overlap buttons or progress. |
| UI-12 | Show size and path so the data can be read and used. | Next to the overall percentage the downloaded/total size is visible; if the total size is less than 1 GB, show MB, otherwise GB. The percentage on the card is placed near the end of the filled part of the progress bar. A long folder path in the details can be expanded and copied in full, without truncating the value in the clipboard. |
| UI-13 | Provide the main actions right next to the torrent. | The card and the details panel have a state-appropriate "Start" or "Stop" action and a "Remove" action. After an action, status, speed and buttons update consistently; removal opens the confirmation from DEL-01 rather than executing immediately. |
| UI-15 | Constrain long values in narrow rows. | Torrent and file names and folder paths do not overflow the sidebar, cards, lists, modal windows or the details panel; visible text is truncated with an ellipsis. The full text is available in a hover tooltip and to assistive technologies. The path in the details can be expanded and copied in full; on-screen truncation does not change the real value. |
| UI-16 | Scroll long lists inside the window. | Switching between "All Torrents", "Downloading", "Completed" and "Seeding", expanding files and a large list do not change the size of the main window. The list and the details panel scroll independently; the sidebar also scrolls if its content does not fit. After choosing another section, the list starts from the top. In a compact window the content remains reachable by scrolling the page. |
| UI-17 | Place the main add action in the top bar. | The "Add Torrent" button sits at the start of the top bar instead of a duplicated application name. Above the list there is no separate block with a title, duplicated controls or a hint; bulk actions appear above the list after torrents are selected. The add action is available in all sections and leads to the same add and metadata check scenario. The current section name is kept for assistive technologies. |
| UI-18 | Show the torrent's files as a tree if there are folders. | In the card and details, nested folders can be expanded and collapsed individually. A folder row shows a play/pause button, the number of selected child files and the total progress by bytes; the button works under TR-07. The tree of an already added torrent has no file or folder checkboxes. Each nested level is indented to the right for the whole row, including the play/pause button, name and progress; child file buttons are not aligned with the parent folder's button. Indentation is consistent in the card and details. Collapsing does not change selection or download. Long names are truncated with access to the full path via tooltip and VoiceOver. For large trees the panel scrolls rather than the main window growing. |
| UI-19 | Show the direction or stop of transfer at the start of the torrent card. | Instead of a file icon, the top-left corner of the card shows a down arrow during download, an up arrow during seeding and a pause sign when transfer is stopped. A completed torrent without seeding also shows the stopped-transfer sign, but the "Completed" text status is kept. The icon updates after a state change, and the meaning of the status does not depend only on the color or shape of the icon. |

All interface strings, including alerts and accessibility text, must be moved into the localization system for the six languages from requirement SET-06.

### 2.4. Removal and file safety

| ID | Requirement | Acceptance criterion |
|---|---|---|
| DEL-01 | Offer "Remove torrent from list" and "Delete torrent and files" separately. | The first option is the default. No other scenario deletes files automatically. |
| DEL-02 | Before deleting files, show a confirmation with the list of files and their location. | The user can cancel the action; the application deletes only files that belong to the selected torrent and does not touch other files in the folder. |
| DEL-03 | Stop the transfer when an entry is removed. | The entry no longer runs in the engine; downloaded files remain on disk unless their deletion was chosen separately. |
| DEL-04 | Handle file access errors without a false success message. | If deleting files failed, the application reports what remained on disk and does not hide the error. |
| DEL-05 | Confirm bulk removal of the selected torrents. | The dialog names the selected entries; by default it removes only the entries and keeps the files. A separate file deletion checkbox shows their list and folders. The user can cancel the action; the application does not touch unselected torrents. |

### 2.5. Settings and local data

| ID | Requirement | Acceptance criterion |
|---|---|---|
| SET-01 | In settings, choose the default download folder via the system folder picker. | The chosen path is visible in settings and preserved after restart. New torrents use it by default; a different folder can be chosen for an individual torrent. Changing the default folder does not move the files of already added torrents. |
| SET-02 | Store the library and transfer policy locally. | SQLite contains the entries, full v1/v2 infohashes, metainfo/magnet, paths, wanted/priorities, manual pause, tags, rules and settings. The engine storage keeps piece completion separately. After a crash, entries are restored without duplicates or file loss; untrusted or changed data is checked, and the manual pause is applied before payload is allowed. |
| SET-03 | Do not turn a missing or inaccessible folder into silent data loss. | The application shows the problem and allows specifying the location again. |
| SET-04 | In settings, choose the appearance: "System", "Light" or "Dark". | The macOS theme is used by default. In "System" mode the interface changes together with the system theme without a restart; a manual choice takes priority over the system and is preserved after restart. All windows, dialogs and states remain readable in both themes. |
| SET-05 | For the default download folder, allow choosing an existing folder or creating a new one by name inside the chosen parent folder. | After successful creation the path is filled into the settings; it becomes the default after saving. Name, access and creation errors are shown to the user, and the previous setting is kept. |
| SET-06 | In settings, choose the interface language: English, Ukrainian, Spanish, German, French or Portuguese. | All six languages are available for all screens, dialogs, messages, errors and accessibility elements. The application takes the initial language from the macOS preferences among the supported ones, otherwise it uses English. A manual choice is applied without a restart and preserved after it; torrent, file and folder names are not translated. |
| SET-07 | Let the user make OctopusTorrent the default application for `.torrent`. | The application registers `.torrent` support in macOS; settings contain a clear path to the system choice of the default application. The user makes the choice, without a silent change of the current default application. After the choice, a double-click on a `.torrent` in Finder opens the add dialog, and file checking and folder selection work the same as for the add button. |
| SET-08 | Provide a global upload toggle for all torrents. | In the "Transfer" section the user turns "Stop upload for all torrents" on or off; the state is preserved after restart and applied to existing and new entries. The interface warns that download speed may drop and some trackers may apply their own rules. This is a local Mac setting. The behavior matches TR-08. |

### 2.6. Distribution and support

| ID | Requirement | Acceptance criterion |
|---|---|---|
| REL-01 | Install the personal build locally. | `wails build` builds the frontend and the Go application into an `.app` that opens on the owner's Mac. A `.dmg` can be prepared for reinstallation; a website is not needed. Go, Node.js and the required native build tools are development tools, not a requirement for running the finished build. |
| REL-02 | Verify the signature and dependencies of the installed build. | The personal build runs on the target Mac without depending on the end user's Homebrew. Developer ID and notarization are checked separately if there is a need to distribute the build. |
| REL-03 | Keep a short local help. | Adding, file selection, folders, pause, seeding, restore and removal are described; technical diagnostics help the owner fix problems. |

### 2.7. Library and local automation

| ID | Requirement | Acceptance criterion |
|---|---|---|
| COL-01 | Tags and smart collections | The user can add several tags to a torrent, and create, rename and delete a collection as a saved filter by tags and status. Collection membership updates after tags or status change; one torrent can be in several collections without duplicating the entry. Deleting a collection does not delete torrents or files. Local search follows NEXT-01. |
| AUTO-01 | Watch folder | The user explicitly chooses a local folder for new `.torrent` files. After a file has finished being written, the application checks its contents, avoids re-importing the same infohash and shows an error for an invalid file. By default the file goes to review; only after automatic adding is separately enabled is a valid torrent added paused. The source `.torrent` is not deleted automatically. |
| AUTO-02 | Rules for new torrents | A rule sets conditions on the name or the add source and actions: assign tags and a local download folder. Rules have a visible priority order and a preview of the result; the user can disable a rule. An unavailable folder or an error does not start the torrent and does not silently move files. An automatically added torrent stays paused until started manually. |

**Implementation order:** basic downloading and reliable local restore → tags and collections (COL-01) → rules and watch folder (AUTO-01–02). These features are available in a single personal version without an artificial limit on the number of entries.

## 3. Local data and privacy

| ID | Requirement | Acceptance criterion |
|---|---|---|
| DATA-01 | Store the library and settings locally. | The application works without registration or its own server; access to features does not depend on an external check. The network is needed for BitTorrent operation, not for opening the list or settings. |
| DATA-02 | Do not collect product analytics. | There is no installation identifier, no conversion events and no automatic sending of torrent names, paths, magnet links or diagnostics. |
| DATA-03 | Have local diagnostics for development. | The log contains time, category, error code and operation ID; tracker URL secrets and full local paths do not get into the regular log. Displaying a problem does not change the state of stored files. |

## 4. Planned after MVP

| ID | Feature | Boundaries |
|---|---|---|
| NEXT-01 | Search in the torrent list | Local search by name, as in the mockup; implemented after the main list works reliably. |
| NEXT-02 | List filters | All, active, completed and seeding. The sections stay in the mockup and are implemented as local filters. |

The "Restore example", error display and offline state buttons under the window are tools of the interactive mockup. In the real application, errors and network state are displayed automatically under UI-10.

A content catalog, sharing the library or downloaded data between Macs, mobile applications and extended network analytics are **not part of the agreed scope of work**. They can be considered only after a separate decision.

## 5. Technical and quality requirements

- **Architecture:** React/TypeScript displays the application's own DTOs and sends commands through Wails. Go services manage the library, files and anacrolix/torrent; SQLite is the source of the stored policy, the engine is the source of actual progress. Long operations run with a managed lifecycle and do not block the UI.
- **Resilience:** network failures, an invalid torrent, lack of space, a path change and a crash must not cause unannounced deletion or re-download of data.
- **Data safety:** no interface action deletes files without an explicit choice. The delete operation must check the path of every file.
- **Accessibility:** the main actions are available from the keyboard; statuses have text labels, not just colors; elements support VoiceOver and system text scaling.
- **Transparency and contrast:** the CSS styling of panels respects reduced transparency and increased contrast. If the system preference cannot be obtained reliably through the WebView, the opaque style is provided by an explicitly documented fallback.
- **Performance:** the list and details remain responsive during downloading; frequent progress updates must not overload React or the Go–TS event channel.
- **Compatibility and dependencies:** the minimum supported macOS version and Mac architectures must be determined before implementation starts; before release, check the terms of use of anacrolix/torrent, Wails and all included components.
- **macOS integration:** register `.torrent` as a supported document type in the application bundle via `CFBundleDocumentTypes`; the system choice of the default application belongs to the user. Verify opening a file from Finder and passing it to the application on cold and repeated launch.

### 5.1. Contracts of the chosen stack

| ID | Requirement | Acceptance criterion |
|---|---|---|
| STACK-01 | Isolate access to native capabilities in Go. | React uses a narrow Wails API; it does not execute arbitrary shell commands and does not delete files through a third-party workaround. Validation of paths, metadata and commands is done in Go independently of UI checks. |
| STACK-02 | Pass consistent Go–TS state snapshots. | The initial snapshot is available through a separate request; events carry a revision/sequence and update only the current state. A missed event or a reconnect is recovered with a full snapshot. React does not resubscribe without cleaning up listeners. |
| STACK-03 | Serialize policy changes of each torrent. | Pause, global upload, wanted and removal have a consistent order in Go. After a slow response the UI does not revert to a stale state. Concurrent commands do not lose the manual pause or the file selection. |
| STACK-04 | Manage SQLite only through the repository. | Schema version, transactions and migrations are verified; a write error is not reported as a successful action. Library data is separated from the payload and from the engine's internal piece completion. |
| STACK-05 | Prove the build and native integration on the owner's Mac. | The `.app` launches with local assets; Finder/magnet, system dialogs, keyboard/VoiceOver and application shutdown are verified in a real Wails window. A frontend browser test does not replace this check. |

### 5.2. AI-first: code for further AI work

The owner defines the desired application behavior and evaluates the result; deep knowledge of Go, React, TypeScript and Wails is not a prerequisite for maintaining the project. We write code on the assumption that the main development, reading, debugging and subsequent changes will be done by AI agents. A new agent must understand the project from the repository without access to previous chats.

The priority is explicit logic, a predictable structure and reproducible verification. Convenience for AI does not cancel the requirements for file safety, transfer correctness and performance. Comments explain decisions and invariants; clear names and small modules explain everyday behavior.

| ID | Requirement | Acceptance criterion |
|---|---|---|
| AI-01 | Give the agent an entry point into the repository. | The root `README.md` explains the purpose, the current state, a map of the main directories and the exact build, run and check commands. `AGENTS.md` contains short working rules, links to the current requirements and architecture, and special rules for the Go–TS bridge and file operations. The documents do not require conversation history. |
| AI-02 | Use unambiguous names and explicit data types. | Names of files, types, methods and variables describe the domain action; identifiers are written in clear English following the usual rules of the language. For bytes, speeds, time, file index and UUID, units and purpose are visible. Significant numbers and modes have named constants or types. Unclear abbreviations are explained locally. |
| AI-03 | Split code by responsibility. | A file or type has one main role. UI, import rules, file access, local storage and the engine have clear boundaries. Large mixed files are split by real tasks; no arbitrary line-count limits or extra layers are introduced without a practical need. |
| AI-04 | Make dependencies and side effects visible. | Components receive the dependencies they need explicitly. Disk reads/writes, network commands and state mutation are separated from computation. There is no hidden global state the agent has to guess. From a specific UI action one can trace the command, the state change and the result. |
| AI-05 | Document risky contracts in place. | For Wails bindings, DTOs/events, goroutines and file operations, the lifecycle, ordering, errors, cancellation and invariants are described. The application's own DTOs cross the Go–TS boundary, without engine objects. A comment explains a non-trivial decision; a public API has a concise contract. |
| AI-06 | Keep decisions and current context in the repository. | Requirements describe behavior, the architecture describes components and flows, and short records in `docs/decisions/` describe important choices and their reasons. The description distinguishes verified facts from assumptions and unfinished prototypes. A change of a contract, structure or build command updates the corresponding document in the same change. |
| AI-07 | Ensure a reproducible build and verification. | The Go toolchain and backend dependencies are pinned in `go.mod`/`go.sum`; the Wails CLI and Node.js versions are documented and reproducible, frontend dependencies are pinned in the lockfile. The README gives exact commands and native prerequisites for the Mac. Generated bindings are reproducible and are not edited by hand. Scripts do not depend on hidden steps or the developer's absolute paths. |
| AI-08 | Verify important behavior and invariants. | Go tests cover import, deduplication, transfer policies, restore and removal; React tests verify actions and accessibility through the test bridge. Real Wails bindings and anacrolix/torrent are verified by separate integration scenarios. For Go, concurrent state changes are verified, including the race detector in a supported configuration. Every change passes TDD and the mandatory 100% coverage under QA-01/QA-02/QA-05; behavioral, integration and visual checks complement each other. |
| AI-09 | Make changes understandable for the next agent and the owner. | A change has a clear goal and a reviewable diff without unrelated rewriting. Git and the report explain the result, the verification method and known limitations. Unfinished places are marked with a local TODO with a reason and a completion criterion, not a vague promise. Behavior is explained to the owner in plain language. |
| AI-10 | Verify the handoff of the project to an agent with a clean context. | Before a main stage is completed, an agent with only the repository can find the entry point, the needed module and the corresponding requirement, run the build/checks and trace one scenario from the UI to the engine. If this requires an unrecorded hint from the owner, the context is added to the corresponding document. |

These rules apply to Go, React/TypeScript, Wails bindings, build configuration, the local DB and helper scripts. Third-party code is not rewritten for the sake of style; its use is isolated by clear contracts. AI-first is a mandatory principle for the implementation and maintenance of all subsequent changes.

### 5.3. Mandatory engineering discipline

These are hard conditions of all tasks and every session. Detailed metrics, the work cycle and measurement boundaries: [TDD_AND_QUALITY.md](../docs/engineering/TDD_AND_QUALITY.md). The agent rules come from [AGENTS.md](../AGENTS.md), the feature index is [docs/README.md](../docs/README.md).

| ID | Requirement | Acceptance criterion |
|---|---|---|
| QA-01 | Mandatory TDD for every change. | RED → GREEN → REFACTOR applies from the first code onward, to fixes, prototypes, configuration and verifiable documentation changes. The report proves the expected failure before the new implementation, the successful check and the refactoring; code is not written first with tests deferred. |
| QA-02 | Always maintain 100% coverage of the project's own executable code. | All defined metrics with a fixed file inventory reach exactly 100% per file and overall before the green step is completed, before integration and before done. The Go and TS metrics are defined in the engineering policy; an empty or stale report is not a success. Do not hide difficult files and do not lower thresholds. |
| QA-03 | Write clean, reusable code following the rules of the stack. | Before a new implementation, existing components and services are found; shared behavior has a single owner. The React/Wails/Go/engine/SQL boundaries are explicit, dependencies are passed explicitly, formatting/types/analyzers are checked. There are no duplicated business rules, mixed catch-all modules or unnecessary abstractions. |
| QA-04 | Document every feature incrementally in docs/. | Before implementation, behavior, architectural impact and tests are described; after, the actual modules, contracts, results and state. docs/README.md and docs/<domain>/<feature>.md are updated in the same change; planned work is not marked as implemented. |
| QA-05 | Automatically block acceptance when quality is violated. | A local command checks tests, exact 100% coverage, completeness of the source inventory, formatting, types/analyzers, bindings and documentation. A missing, failed or not run mandatory check blocks integration/done; ignoring errors and narrowing the scope are forbidden. |
| QA-06 | Verify behavior and real boundaries, not just the percentage. | Tests have meaningful assertions and negative cases. Network/disk/SQLite/macOS are verified at the proper boundaries; a test double is not declared a real integration. A regression test for a bug starts by reproducing it; 100% does not replace the acceptance criteria. |
| QA-07 | Analyze the architecture at the start of every session. | The agent reads the current architecture, feature documents and code changes, determines impact, reuse, risks and needed refactoring, and records a short plan before editing. Continuing a task in a new session goes through this analysis again; significant decisions update the architecture/ADR. |
| QA-08 | Do not accumulate technical debt; refactor while working. | In the affected scenario, on green tests and with 100% coverage, duplication, wrong boundaries and temporary workarounds are eliminated. New debt, a needed stub, deferred tests/documents or a suppressed error forbid done. Inability to finish yields blocked with a remediation plan, not a promise to fix it later. |

## 6. End-to-end acceptance scenarios

1. **First launch:** opens the personal build, adds a `.torrent`, chooses files and a folder, starts the download; after restart the list, file selection and pause are preserved.
2. **Several torrents:** adds a second, third and further entries without interrupting earlier transfers and without additional access screens.
3. **Magnet:** pastes a link, waits for the real metadata, chooses files, downloads and seeds if needed.
4. **Removing an entry:** confirms removal without the files checkbox; the entry disappears, the transfer stops, the files remain.
5. **Deleting files:** separately chooses file deletion and sees the list; unrelated files in the folder are not touched, a deletion error is shown explicitly.
6. **Bulk actions:** selects several torrents via the persistent checkboxes, starts or stops only them; "Clear Selection" removes the action bar. Bulk removal has a confirmation.
7. **Individual file and folder:** excludes a file or a whole nested folder from the selection, sees "Skipped", then selects it again. Other files keep downloading; existing bytes remain. Actions are consistent in the card and details.
8. **Drag-and-drop:** a valid `.torrent` dropped on the sidebar zone opens the preview; a file of another type, a corrupted file and several files produce a clear rejection.
9. **New folder:** in the add dialog or settings, chooses a parent folder, gives a name and confirms creation. An error does not change the previous path.
10. **Default folder:** a settings change applies to future torrents; files of existing entries are not moved.
11. **Theme and accessibility:** the system, light and dark themes work with the keyboard, VoiceOver, reduced transparency and text scaling.
12. **Language:** switches between the six agreed languages; UI texts change, while torrent, file and folder names stay original. The choice is preserved after launch.
13. **Alerts and offline:** without internet the library and settings are available. The alert explains that network transfer has stopped; after the network returns, active torrents resume work and the manual pause is preserved.
14. **Finder:** after the system default application is assigned, a double-click on a `.torrent` opens the same preview on cold and repeated launch.
15. **Collections:** tags and statuses automatically determine the membership of a saved filter; deleting a collection does not delete torrents or files.
16. **Rules and watch folder:** checking a new `.torrent`, duplicates and rules works locally; explicitly enabled automatic adding creates a paused torrent.
17. **Long lists and names:** many entries and nested files do not change the window size; panels scroll, long names are truncated with access to the full value.
18. **Global upload stop:** current and new torrents obey TR-08; download continues, and turning the mode off does not cancel manual pauses. The real result is confirmed by a network prototype.
19. **Status icons:** after pause, resume and the start of seeding, the icon in the card matches UI-19, and the status and speeds are updated consistently.

## 7. Parameters fixed on the technical prototype

1. The macOS version and architecture of the owner's Mac; support for other Macs is needed only if a separate need arises.
2. Specific versions of Wails v2, anacrolix/torrent, Go and frontend dependencies after a trial build; the base stack is already chosen.
3. The result of the prototype for the global upload stop without stopping download.
4. Behavior when downloaded files are moved, changed or partially lost.
5. The SQLite driver, schemas and migrations; the engine's storage/piece-completion backend and verification of its restore after a crash.
6. The method of local installation and signing of the personal build; public distribution is not a condition for completing the MVP.

Design reference: [Apple Human Interface Guidelines — Materials](https://developer.apple.com/design/human-interface-guidelines/materials), [Sidebars](https://developer.apple.com/design/human-interface-guidelines/sidebars), [Toolbars](https://developer.apple.com/design/human-interface-guidelines/toolbars).

System file opening: [Apple — CFBundleDocumentTypes](https://developer.apple.com/documentation/bundleresources/information-property-list/cfbundledocumenttypes), [Apple — choosing the default application for files](https://support.apple.com/guide/mac-help/choose-an-app-to-open-a-file-on-mac-mh35597/mac).
