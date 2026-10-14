package devtool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
