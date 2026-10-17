package library

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The SQLite release that fixed the WAL-reset bug (https://www.sqlite.org/wal.html, section 11).
var walResetFixed = [3]int{3, 51, 3}

func TestTheBundledSQLiteHasTheWALResetFix(t *testing.T) {
	version, err := SQLiteVersion(deadline(t, 5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		t.Fatalf("SQLiteVersion() = %q, want x.y.z", version)
	}
	var got [3]int
	for i, p := range parts {
		if got[i], err = strconv.Atoi(p); err != nil {
			t.Fatalf("SQLiteVersion() = %q: %v", version, err)
		}
	}
	for i := range got {
		if got[i] != walResetFixed[i] {
			if got[i] < walResetFixed[i] {
				t.Fatalf("SQLite %s has the WAL-reset bug; at least %d.%d.%d is required", version, walResetFixed[0], walResetFixed[1], walResetFixed[2])
			}
			break
		}
	}
	t.Logf("SQLite %s", version)
}

func TestTheVersionQueryReportsAFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SQLiteVersion(ctx); !errors.Is(err, ErrCanceled) {
		t.Fatalf("SQLiteVersion(cancelled) = %v, want ErrCanceled", err)
	}
}
