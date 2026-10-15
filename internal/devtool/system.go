package devtool

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
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

func (OSSystem) Capture(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func (OSSystem) RemoveAll(path string) error { return os.RemoveAll(path) }

func (OSSystem) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }

func (OSSystem) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names, err
}

func (OSSystem) Sleep(d time.Duration) { time.Sleep(d) }

// httpClient bounds every registry request in time and keeps it on the host that was asked.
var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if first := via[0].URL; req.URL.Scheme != first.Scheme || req.URL.Host != first.Host {
			return fmt.Errorf("redirect to another host: %s", req.URL)
		}
		return nil
	},
}

// maxResponseBytes bounds a registry answer; the largest npm documents are tens of megabytes.
var maxResponseBytes int64 = 256 << 20

func (OSSystem) Get(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err == nil && int64(len(body)) > maxResponseBytes {
		err = fmt.Errorf("GET %s: the answer is larger than %d bytes", url, maxResponseBytes)
	}
	return body, err
}

func (OSSystem) Now() time.Time { return time.Now() }
