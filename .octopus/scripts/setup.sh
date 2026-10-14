#!/bin/sh
# Prepares a workspace: locked frontend dependencies, frontend/dist and the Go module cache.
# The logic is Go (cmd/devtool, internal/devtool) so it is tested and measured; this file only hands over.
set -eu
go build -o build/bin/devtool ./cmd/devtool
exec build/bin/devtool workspace setup
