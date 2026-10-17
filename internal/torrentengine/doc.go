// Package torrentengine will hold our own engine contract and its anacrolix adapter (task 018).
//
// Until then it holds only engine contract tests: lifecycle_test.go (task 007) proves, on the pinned
// anacrolix/torrent version, the creation, use and shutdown of the engine objects the adapter will
// own, and storage_test.go (task 009) what the engine's own storage constructors do with a completion
// store they cannot open. They turn red when an engine upgrade changes any of it. The rules the
// adapter must follow are in docs/decisions/engine-lifecycle.md and docs/decisions/engine-storage.md.
package torrentengine
