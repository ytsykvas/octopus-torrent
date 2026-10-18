//go:build darwin

package platform

/*
#cgo darwin LDFLAGS: -framework Foundation
#include <stdlib.h>
#include "trash_darwin.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
	"unsafe"
)

// systemTrasher moves files to the macOS Trash with NSFileManager (trash_darwin.m).
type systemTrasher struct{}

// NewSystemTrasher returns the real macOS Trasher the application wires into the facade. Tests inject
// a fake instead.
func NewSystemTrasher() Trasher { return systemTrasher{} }

// TrashFile moves one file to the Trash and returns its new location there, or the system error. It
// validates the path in Go first: an empty, relative, NUL-bearing or non-UTF-8 path would make the
// native NSString/NSURL call abort the process, and C.CString would truncate a NUL.
func (systemTrasher) TrashFile(absPath string) (string, error) {
	if absPath == "" || !filepath.IsAbs(absPath) || strings.IndexByte(absPath, 0) >= 0 || !utf8.ValidString(absPath) {
		return "", fmt.Errorf("%w: %q", ErrBadPath, absPath)
	}
	cpath := C.CString(absPath)
	defer C.free(unsafe.Pointer(cpath))
	var cerr *C.char
	loc := C.trash_file(cpath, &cerr)
	if loc == nil {
		msg := C.GoString(cerr)
		C.free(unsafe.Pointer(cerr))
		return "", errors.New(msg)
	}
	defer C.free(unsafe.Pointer(loc))
	return C.GoString(loc), nil
}
