// Package pendingmagnet fetches a magnet's metadata with no payload moving and, once the user
// confirms the import, puts the payload exactly in the chosen folder (task 011, ADR
// magnet-pending-storage).
//
// The pending torrent's storage is a filestore.Deferred: it creates nothing and refuses every read,
// write and mark until Confirm attaches the final folder's storage. Both directions stay disallowed
// until then. The engine applies a received info before Fetch sees it, and some info panics it
// (findings 33–35), so the storage admits only info that infocheck accepts within the request's
// limits: the engine opens the storage before it applies the info and discards refused metadata. The
// engine also accepts any info dictionary for a v1 infohash (finding 11), so Fetch hashes the
// received bytes against the requested infohash and drops the torrent on a mismatch; nothing was
// written. Confirming needs neither a Drop nor a second add: the engine looks a piece's storage up on
// every access, so the connections stay and no other peer's metadata can take the torrent's place.
//
// Ownership follows the 007 rules: an add that finds the infohash already in the client gets
// ErrDuplicate and never drops the torrent it did not create.
//
// It is a prototype that the engine adapter (018) and the import coordinator (022) build on; the
// product does not link it yet.
package pendingmagnet
