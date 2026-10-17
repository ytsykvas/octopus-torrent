// Package filestore is the storage the engine adapter (task 018) gives every torrent: the payload
// goes into the folder the user chose, the verified-piece marks go into our own SQLite file, and
// nothing falls back silently. It is the prototype task 009 builds on the pinned engine's file
// storage; the decision and its evidence are in docs/decisions/engine-storage.md.
//
// The engine's own helpers hide three failures that this package turns into errors:
//   - storage.NewFile and friends fall back to an in-memory completion when their database cannot
//     be opened, so a restart forgets every piece;
//   - classic file IO recreates a deleted save folder, or the mount point of a detached volume, on
//     the disk it finds, and writes the payload there;
//   - a failed write of a completion mark is only logged.
package filestore
