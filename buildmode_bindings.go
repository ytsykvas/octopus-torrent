//go:build bindings

package main

import "octopustorrent/internal/datadir"

// The build that generates the bindings: Wails runs main to read the bound methods on every wails
// build, generate module and wails dev, so run opens no data folder and no library
// (docs/decisions/library-schema-and-data-folder.md). resolveDataDir is never called here; it is
// the development build's resolver, so a reordered run fails closed instead of opening the owner's
// folder.
const generatingBindings = true

var resolveDataDir = datadir.FromOverride
