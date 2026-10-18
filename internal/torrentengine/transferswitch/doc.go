// Package transferswitch drives the pinned engine's per-torrent transfer switches so that their
// effect is proven, not assumed (task 010, ADR global-upload-stop).
//
// Three engine defects shape it:
//   - A connection's writer can sleep through a lost wake-up (findings 14 and 22): every client must
//     set ClientConfig.KeepAliveTimeout to WakeInterval, which bounds that sleep.
//   - Returning from DisallowDataUpload is not a measured stop: StopUpload confirms it from the
//     torrent's own payload counter.
//   - Allowing download again while the cancelled requests still await their Reject leaves the
//     torrent with no request for good (finding 12), also after the first bytes of the resume:
//     ResumeDownload watches the received counter until the torrent is complete and nudges it
//     whenever payload stops.
//
// It is a prototype that the engine adapter (018) and the global upload policy (053) build on; the
// product does not link it yet.
package transferswitch
