// Command devtool runs the repository's development tooling; see internal/devtool.
package main

import (
	"os"

	"octopustorrent/internal/devtool"
)

func main() {
	os.Exit(devtool.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv, devtool.OSSystem{}))
}
