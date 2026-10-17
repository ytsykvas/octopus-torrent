// Package classicio makes the process use the engine's classic file IO. The engine picks its file IO
// in an init function, from TORRENT_STORAGE_DEFAULT_FILE_IO, before any of our code runs; its
// default, mmap, fails a chunk that spans a zero-length file (task 006) and never unmaps payload
// files (task 007). So a process that links the engine must start with the variable set. This
// package imports no engine package, so any main can call it first.
package classicio

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// Variable is the environment variable the engine reads.
const Variable = "TORRENT_STORAGE_DEFAULT_FILE_IO"

// Restarted marks a process that Require has already started again, so that a launcher which drops
// Variable cannot make the process restart for ever.
const Restarted = "OCTOPUS_CLASSIC_IO_RESTARTED"

// Process is what Require needs from the running process, so a test can replace it.
type Process struct {
	LookupEnv  func(string) (string, bool)
	Executable func() (string, error)
	// Exec replaces the process image; it returns only when that failed.
	Exec    func(path string, argv, env []string) error
	Args    []string
	Environ []string
}

// Current is the running process.
func Current() Process {
	return Process{
		LookupEnv:  os.LookupEnv,
		Executable: os.Executable,
		Exec:       syscall.Exec,
		Args:       os.Args,
		Environ:    os.Environ(),
	}
}

// Require returns nil when the process already runs with classic file IO. When the variable is not
// set, it starts the same executable again with the same arguments, the variable and a marker added;
// Exec does not return then, and Require returns only an error of that attempt. Any other value is
// an error: the product does not run on mmap. A process that carries the marker but not the variable
// is an error too.
//
// The engine's own init functions have run before Require does. A caller that refuses GOPPROF must do
// so before it calls Require, and the restarted process inherits what the first one had.
func Require(p Process) error {
	value, set := p.LookupEnv(Variable)
	if set {
		if value != "classic" {
			return fmt.Errorf("%s=%q: the application needs classic file IO; unset it", Variable, value)
		}
		return nil
	}
	if _, restarted := p.LookupEnv(Restarted); restarted {
		return fmt.Errorf("the process was restarted with %s=classic, but %s is not set: something removed it", Variable, Variable)
	}
	path, err := p.Executable()
	if err != nil {
		return fmt.Errorf("restart with %s=classic: find the executable: %w", Variable, err)
	}
	env := append(append([]string(nil), p.Environ...), Variable+"=classic", Restarted+"=1")
	if err := p.Exec(path, p.Args, env); err != nil {
		return fmt.Errorf("restart with %s=classic: %w", Variable, err)
	}
	return errors.New("the process was not replaced by its restart")
}
