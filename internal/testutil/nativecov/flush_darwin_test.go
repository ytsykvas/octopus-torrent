//go:build darwin

package nativecov

import "testing"

// Flush is a no-op unless the binary was built with coverage instrumentation; calling it here covers
// its one Go line. A test package's TestMain calls it for real before os.Exit.
func TestFlushIsCallable(t *testing.T) { Flush() }
