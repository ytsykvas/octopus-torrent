# Target macOS and application identity

**Status:** `accepted` (the minimum version is reviewed by [066](../../tasks/066-personal-build-install-license-audit.md))  
**Date:** 2026-10-14  
**Task:** [003](../../tasks/003-wails-macos-build-spike.md)  
**Register item:** O1 from the [baseline description](project-baseline.md#6-open-decision-register)  
**Requirements:** REL-01, REL-02, STACK-05

## Question

Which macOS version and Mac architecture the application declares as supported, and which bundle identifier it registers with the system. The identifier is long-lived: the default application for `.torrent`, the data folder, macOS permissions.

## Decision

- **Supported configuration is only the verified one:** macOS 27.0.1 (26A434), Apple Silicon `arm64` (Apple M4). `LSMinimumSystemVersion` = `27.0` in `build/darwin/Info.plist` and `Info.dev.plist`. Older macOS versions and Intel Macs are not declared until they are verified.
- **Bundle architecture:** `arm64` only (`darwin/arm64`, the default `wails build` target on this Mac). A Universal build is not needed (requirements §1).
- **Bundle identifier:** `dev.tsykvas.octopustorrent` — owner's decision of 2026-10-14. The Wails template `com.wails.<name>` is replaced in both plists.
- **Signing:** ad-hoc, automatically during `wails build` ("Self-signing application"); `TeamIdentifier` is not set. Gatekeeper (`spctl -a`) rejects such a bundle, but a locally built `.app` has no quarantine attribute and opens from Finder. Developer ID and notarization are not needed for a personal build (REL-02) and are considered in 066.

## Alternatives considered

| Option | Advantages | Disadvantages | Why not chosen |
|---|---|---|---|
| Wails default `LSMinimumSystemVersion 10.13.0` | Nothing to change | Declares compatibility that nobody has verified; Go 1.27 requires a newer macOS | Violates the rule "unverified is not called supported" |
| Universal (`arm64` + `amd64`) | Also works on Intel Macs | Longer build, unverified target | Other Macs are not in scope |
| `com.wails.OctopusTorrent` | Default value | The namespace does not belong to the owner | Owner's decision is `dev.tsykvas.octopustorrent` |

## Verified grounds

[Report 003](../verification/003-wails-macos-build-spike.md): `plutil -p` shows `CFBundleIdentifier = dev.tsykvas.octopustorrent` and `LSMinimumSystemVersion = 27.0`; `lipo -archs` → `arm64`; `codesign -dv` → `Signature=adhoc`, `codesign --verify --deep --strict` — `pass`; `spctl -a` → `rejected`; launch from Finder (owner's double-click) and via `open` — `pass`.

## Consequences

- When installed on a different macOS version, the system will refuse to launch it until 066 verifies and lowers the minimum.
- Changing the bundle ID after the library exists requires data migration and choosing the default application again, so the ID is considered stable.
- `CFBundleDocumentTypes` for `.torrent` and the `magnet:` scheme are added to these plists in [036](../../tasks/036-finder-magnet-open-queue.md) (O13).
