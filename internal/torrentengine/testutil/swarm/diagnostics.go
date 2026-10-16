package swarm

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var ipText = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b|\[[0-9A-Fa-f:.]+\]`)

// NonLoopback returns every IP address in the text that is not a loopback address.
func NonLoopback(text string) []string {
	var found []string
	for _, m := range ipText.FindAllString(text, -1) {
		m = strings.Trim(m, "[]")
		if ip := net.ParseIP(m); ip != nil && !ip.IsLoopback() {
			found = append(found, m)
		}
	}
	return found
}

// WaitEngineStopped waits until no goroutine runs engine code, so a closed swarm left nothing
// behind. When ctx ends first it returns the stacks of those goroutines.
func WaitEngineStopped(ctx context.Context) error {
	for stacks := engineStacks(); stacks != ""; stacks = engineStacks() {
		if err := pause(ctx, 20*time.Millisecond); err != nil {
			return fmt.Errorf("engine goroutines still running: %w\n%s", err, stacks)
		}
	}
	return nil
}

func engineStacks() string {
	buf := make([]byte, 8<<20)
	buf = buf[:runtime.Stack(buf, true)]
	var stacks []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "github.com/anacrolix/") {
			stacks = append(stacks, g)
		}
	}
	return strings.Join(stacks, "\n\n")
}
