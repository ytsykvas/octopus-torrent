package devtool

import (
	"os"
	"os/exec"
	"syscall"
)

// OSSystem is the real System.
type OSSystem struct{}

func (OSSystem) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (OSSystem) WriteFile(name string, data []byte) error { return os.WriteFile(name, data, 0o644) }

func (OSSystem) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func (OSSystem) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

func (OSSystem) Exec(path string, argv []string) error {
	return syscall.Exec(path, argv, os.Environ())
}
