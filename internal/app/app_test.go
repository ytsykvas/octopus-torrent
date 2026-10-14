package app

import (
	"encoding/json"
	"runtime"
	"testing"
)

func TestGetRuntimeInfoReportsCompiledGoRuntime(t *testing.T) {
	info := New().GetRuntimeInfo()

	want := RuntimeInfo{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if info != want {
		t.Fatalf("GetRuntimeInfo() = %+v, want %+v", info, want)
	}
}

func TestRuntimeInfoJSONUsesBridgeFieldNames(t *testing.T) {
	encoded, err := json.Marshal(RuntimeInfo{GoVersion: "go1.0", OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	const want = `{"goVersion":"go1.0","os":"darwin","arch":"arm64"}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}
