//go:build dev && !bindings

package main

import "octopustorrent/internal/datadir"

// The development build (wails dev): it refuses to start without OCTOPUS_DATA_DIR, so it never opens
// the owner's library (docs/decisions/library-schema-and-data-folder.md).
const generatingBindings = false

var resolveDataDir = datadir.FromOverride
