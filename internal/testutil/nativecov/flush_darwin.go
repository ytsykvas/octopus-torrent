//go:build darwin

// Package nativecov lets a package's test binary write its clang source-coverage profile. A Go test
// binary exits through os.Exit, which skips the C runtime's atexit writer, so the profile would be
// empty; a test package's TestMain calls Flush before exiting. It is test support (never linked by
// the product), so the LLVM profile symbol does not ship in a release binary. The gate's native
// coverage step (task 069) reads the profiles this writes.
package nativecov

/*
// __llvm_profile_write_file writes the clang coverage counters. The profile runtime defines it only
// in an instrumented build (-fprofile-instr-generate); a weak no-op definition here lets a normal
// build link, and the runtime's strong definition overrides it when instrumented.
__attribute__((weak)) int __llvm_profile_write_file(void) { return 0; }

static int octo_flush_coverage(void) { return __llvm_profile_write_file(); }
*/
import "C"

// Flush writes the native coverage profile when the binary was built with instrumentation, and does
// nothing otherwise.
func Flush() { C.octo_flush_coverage() }
