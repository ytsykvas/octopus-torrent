// Package torrentengine will hold our own engine contract and its anacrolix adapter (task 018).
//
// Until then it holds only the engine contract tests of task 007: they prove, on the pinned
// anacrolix/torrent version, the creation, use and shutdown of the engine objects the adapter will
// own, and they turn red when an engine upgrade changes any of it. The rules the adapter must
// follow are in docs/decisions/engine-lifecycle.md.
package torrentengine
