# Action: review a branch / diff — attack surface and data safety

The **Attack surface & data safety** lens only; blockers and reporting rules are in
[review.md](review.md) — read that first, as exploitability. Each finding needs the concrete input or
sequence that triggers it.

---

### Untrusted input

- `.torrent` files, magnet links, tracker responses and peer data are hostile input. Look for
  unbounded allocations, path components such as `..`, absolute paths or symlink targets taken from
  metadata, and file names rendered into the UI.
- Torrent names and paths reach React as text: flag `dangerouslySetInnerHTML`, `innerHTML` or URL
  construction from them.
- A watch folder, Finder open or drag and drop delivers paths the user did not type: same validation as
  any other path (STACK-01).

### Files and paths

- Every write or deletion is limited to the chosen save path after resolving symlinks; collisions and
  files shared between torrents are detected before acting (DEL-02).
- Deletion goes to the macOS Trash through the platform adapter, only for the confirmed list, and a
  partial failure reports what remained (DEL-04).
- Temporary files are created with restrictive permissions and removed on every path.

### The Wails boundary

- Every facade method re-validates its DTO in Go; IDs are looked up, never trusted to index directly.
- No facade method executes a shell command or opens an arbitrary URL/path on request from the UI.
- The frontend loads only bundled local assets; no remote scripts, styles or fonts.

### Privacy (DATA-01–03)

- No analytics, installation identifiers or automatic sending of torrent names, paths, magnet links or
  diagnostics.
- Regular logs carry time, category, error code and operation ID — not tracker URL secrets (passkeys)
  or full local paths.

### Supply chain

- New or bumped dependencies: published at least 14 days ago, not a pre-release, exact version,
  `npm audit` and `govulncheck` clean, install scripts only via `allowScripts`.
- No `GOSUMDB=off`, `GOINSECURE`, `GONOSUMDB`, `npx` downloads or global npm installs in scripts or
  documentation. The `release-guard` hook enforces this for agents; review catches it in committed text.

### Secrets

- No credentials, tokens or private keys in source, docs or test data.

---

Report as in [review.md](review.md), with the attack scenario for each finding. List what you
confirmed is safe as well as what is broken.
