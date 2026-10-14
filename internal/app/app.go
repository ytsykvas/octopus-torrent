// Package app is the Wails facade (AppAPI): the only Go type whose exported
// methods are bound to the frontend. Methods validate input, delegate to
// services and return own DTOs; engine objects never cross this boundary.
package app

import "runtime"

// RuntimeInfo describes the Go runtime compiled into the running application.
// It is a bridge DTO: JSON field names are the TypeScript contract.
type RuntimeInfo struct {
	GoVersion string `json:"goVersion"` // e.g. "go1.27.2"
	OS        string `json:"os"`        // GOOS, e.g. "darwin"
	Arch      string `json:"arch"`      // GOARCH, e.g. "arm64"
}

// App is the facade bound to the frontend through Wails.
type App struct{}

// New creates the facade.
func New() *App {
	return &App{}
}

// GetRuntimeInfo reports the Go runtime embedded in the binary. It proves the
// React → bindings → Go path in a packaged .app without any external process.
func (a *App) GetRuntimeInfo() RuntimeInfo {
	return RuntimeInfo{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
}
