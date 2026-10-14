#!/bin/sh
# Starts wails dev on the runner's port (OCTOPUS_PORT or CONDUCTOR_PORT).
# The logic is Go (cmd/devtool, internal/devtool) so it is tested and measured; this file only hands over.
set -eu
go build -o build/bin/devtool ./cmd/devtool
exec build/bin/devtool workspace run
