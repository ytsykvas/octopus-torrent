package devtool

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOSSystemReadsFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".npmrc")
	if err := os.WriteFile(path, []byte("min-release-age=14\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := OSSystem{}.ReadFile(path)
	if err != nil || string(got) != "min-release-age=14\n" {
		t.Errorf("ReadFile = %q, %v", got, err)
	}
}

func TestOSSystemWritesFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.toml")
	if err := (OSSystem{}).WriteFile(path, []byte("name = \"x\"\n")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "name = \"x\"\n" {
		t.Errorf("written %q, %v", got, err)
	}
}

func TestOSSystemRunReportsTheExitStatus(t *testing.T) {
	if err := (OSSystem{}).Run("true"); err != nil {
		t.Errorf("Run(true) = %v", err)
	}
	if err := (OSSystem{}).Run("false"); err == nil {
		t.Error("Run(false) succeeded")
	}
}

func TestOSSystemOutputCapturesStdout(t *testing.T) {
	got, err := OSSystem{}.Output("echo", "hello")
	if err != nil || got != "hello\n" {
		t.Errorf("Output = %q, %v", got, err)
	}
}

// Replacing the test process is proven by the cmd/devtool binary test; here only the failure path
// can run in-process.
func TestOSSystemExecFailsForAMissingProgram(t *testing.T) {
	err := OSSystem{}.Exec(filepath.Join(t.TempDir(), "missing"), []string{"missing"})
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("Exec(missing) = %v, want a no-such-file error", err)
	}
}

func TestOSSystemCaptureCombinesBothStreams(t *testing.T) {
	got, err := OSSystem{}.Capture("sh", "-c", "echo out; echo err >&2; exit 3")
	if err == nil || !strings.Contains(got, "out\n") || !strings.Contains(got, "err\n") {
		t.Errorf("Capture = %q, %v; want both streams and the exit error", got, err)
	}
}

func TestOSSystemManagesDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "coverage", "unit")
	sys := OSSystem{}
	if err := sys.MkdirAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "covmeta.1"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if names, err := sys.ReadDir(dir); err != nil || strings.Join(names, " ") != "covmeta.1" {
		t.Errorf("ReadDir = %q, %v", names, err)
	}
	if err := sys.RemoveAll(filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.ReadDir(dir); err == nil {
		t.Error("ReadDir after RemoveAll succeeded")
	}
}

func TestOSSystemSleeps(t *testing.T) {
	start := time.Now()
	OSSystem{}.Sleep(time.Millisecond)
	if time.Since(start) < time.Millisecond {
		t.Error("Sleep returned early")
	}
}

func TestOSSystemGetsURLsAndRejectsErrorStatuses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/react" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"time":{}}`))
	}))
	defer server.Close()
	if body, err := (OSSystem{}).Get(server.URL + "/react"); err != nil || string(body) != `{"time":{}}` {
		t.Errorf("Get = %q, %v", body, err)
	}
	if _, err := (OSSystem{}).Get(server.URL + "/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Get(missing) = %v, want a 404 error", err)
	}
	if _, err := (OSSystem{}).Get("http://127.0.0.1:1/unreachable"); err == nil {
		t.Error("Get(unreachable) succeeded")
	}
	if now := (OSSystem{}).Now(); time.Since(now) > time.Minute {
		t.Errorf("Now = %v", now)
	}
}

// The registry answers stay with the host that was asked and within a size bound.
func TestOSSystemGetStaysOnTheHostAndBoundsTheAnswer(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "elsewhere") }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, other.URL+"/x", http.StatusFound)
		case "/here":
			http.Redirect(w, r, "/big", http.StatusFound)
		default:
			fmt.Fprint(w, strings.Repeat("x", 11))
		}
	}))
	defer server.Close()
	defer func(limit int64) { maxResponseBytes = limit }(maxResponseBytes)
	maxResponseBytes = 10
	if _, err := (OSSystem{}).Get(server.URL + "/away"); err == nil || !strings.Contains(err.Error(), "redirect to another host") {
		t.Errorf("Get(redirect away) = %v, want a refused redirect", err)
	}
	if _, err := (OSSystem{}).Get(server.URL + "/here"); err == nil || !strings.Contains(err.Error(), "larger than 10 bytes") {
		t.Errorf("Get(too large) = %v, want a size error", err)
	}
	maxResponseBytes = 11
	if body, err := (OSSystem{}).Get(server.URL + "/here"); err != nil || len(body) != 11 {
		t.Errorf("Get(redirect on the same host) = %q, %v", body, err)
	}
}
