// Package app is the Wails facade (AppAPI): the only Go type whose exported
// methods are bound to the frontend. Methods validate input, delegate to
// services and return own DTOs; engine objects never cross this boundary.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
)

// RuntimeInfo describes the Go runtime compiled into the running application.
// It is a bridge DTO: JSON field names are the TypeScript contract.
type RuntimeInfo struct {
	GoVersion string `json:"goVersion"` // e.g. "go1.27.2"
	OS        string `json:"os"`        // GOOS, e.g. "darwin"
	Arch      string `json:"arch"`      // GOARCH, e.g. "arm64"
}

// App is the facade bound to the frontend through Wails. Every exported method is callable from
// JavaScript, so lifecycle hooks (shutdown) live outside it.
type App struct {
	ledger *operations.Ledger
}

// New creates the facade; ledger runs and records its operations and owns the runtime ID.
func New(ledger *operations.Ledger) *App {
	return &App{ledger: ledger}
}

// GetRuntimeInfo reports the Go runtime embedded in the binary. It proves the
// React → bindings → Go path in a packaged .app without any external process.
func (a *App) GetRuntimeInfo() RuntimeInfo {
	return RuntimeInfo{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// GetLibrarySnapshot returns the full state of the library, authoritative for this runtime. No
// library exists before task 034, so it is this runtime at revision 0 with no torrents; 034 changes
// where the data comes from, not this contract.
func (a *App) GetLibrarySnapshot() contracts.LibrarySnapshot {
	return contracts.LibrarySnapshot{RuntimeID: a.ledger.RuntimeID()}
}

// GetOperation returns the current status of an operation accepted by this runtime. An unknown ID
// is operation.notFound naming this runtime: if the operation was accepted by another runtime, its
// outcome is unknown and the frontend reads the full state instead of resending the command.
func (a *App) GetOperation(operationID string) (contracts.OperationStatus, error) {
	if problem := contracts.CheckUUID("operationID", operationID); problem != nil {
		return contracts.OperationStatus{}, problem
	}
	status, problem := a.ledger.Get(contracts.OperationID(operationID))
	if problem != nil {
		return status, problem
	}
	return status, nil
}

// ErrorFormatter returns the Wails error formatter. The Wails JavaScript runtime rejects a call
// with new Error(formatted), which keeps only text, so the AppError crosses as its JSON text and the
// bridge parses Error.message. An error without a code of its own becomes internal.unexpected; its
// cause is written to diagnostics (stderr until local diagnostics, task 017) and never to the UI.
func ErrorFormatter(diagnostics io.Writer) func(error) any {
	return func(err error) any {
		var problem *contracts.AppError
		if !errors.As(err, &problem) || problem == nil {
			// Quoted: a cause may carry torrent-controlled text, and a newline must not forge a line.
			fmt.Fprintf(diagnostics, "OctopusTorrent: unexpected error: %q\n", fmt.Sprint(err))
			problem = contracts.NewAppError(contracts.CodeInternalUnexpected, nil)
		}
		text, _ := json.Marshal(problem) // strings and a map of strings always encode
		return string(text)
	}
}
