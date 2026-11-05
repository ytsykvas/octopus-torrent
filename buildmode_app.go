//go:build !dev && !bindings

package main

import "octopustorrent/internal/datadir"

// The application build: it opens the data folder of the environment, the owner's by default
// (docs/decisions/library-schema-and-data-folder.md).
const generatingBindings = false

var resolveDataDir = datadir.FromEnvironment
