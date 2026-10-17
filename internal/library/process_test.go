package library

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The child processes of these tests: another application instance that holds the write lock, and
// one that commits until it is killed.
const (
	lockChildEnv   = "OCTOPUS_LIBRARY_LOCK_CHILD"
	commitChildEnv = "OCTOPUS_LIBRARY_COMMIT_CHILD"
)

// child starts this test binary as a child running only the named test, with marker=path set. Its
// stdout is returned line by line; it is killed when the test ends.
func child(t *testing.T, name, marker, path string) (*exec.Cmd, io.WriteCloser, *bufio.Scanner) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.timeout=60s")
	cmd.Env = append(os.Environ(), marker+"="+path)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd, stdin, bufio.NewScanner(stdout)
}

// await reads the child's lines until one starts with prefix.
func await(t *testing.T, lines *bufio.Scanner, prefix string) string {
	t.Helper()
	for lines.Scan() {
		if strings.HasPrefix(lines.Text(), prefix) {
			return lines.Text()
		}
	}
	t.Fatalf("the child ended before printing %q: %v", prefix, lines.Err())
	return ""
}

func TestAnotherProcessHoldingTheLockMakesAWriteBusy(t *testing.T) {
	if path := os.Getenv(lockChildEnv); path != "" {
		holdLock(t, path)
		return
	}
	replace(t, &busyTimeout, 200*time.Millisecond)
	s, path := withTable(t)
	cmd, stdin, lines := child(t, "TestAnotherProcessHoldingTheLockMakesAWriteBusy", lockChildEnv, path)
	await(t, lines, "locked")
	start := time.Now()
	err := s.Write(deadline(t, 5*time.Second), func(Tx) error { return nil })
	took := time.Since(start)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Write while another process holds the lock = %v, want ErrBusy", err)
	}
	if took < busyTimeout || took > 2*time.Second {
		t.Errorf("ErrBusy after %v, want between %v and 2s", took, busyTimeout)
	}
	// Closing stdin lets the child commit its row and exit.
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the child: %v", err)
	}
	must(t, s, "INSERT INTO rows(n) VALUES (2)")
	if n := count(t, s); n != 2 {
		t.Fatalf("%d rows after the other process committed, want 2", n)
	}
}

// holdLock is the lock child: it takes the write lock, says so, and commits one row once its stdin
// is closed.
func holdLock(t *testing.T, path string) {
	s := open(t, path)
	err := s.Write(context.Background(), func(tx Tx) error {
		fmt.Println("locked")
		_, _ = io.Copy(io.Discard, os.Stdin)
		_, err := tx.Exec("INSERT INTO rows(n) VALUES (1)")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestEveryAcknowledgedCommitSurvivesAKilledProcess(t *testing.T) {
	if path := os.Getenv(commitChildEnv); path != "" {
		commitForever(t, path)
		return
	}
	s, path := withTable(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cmd, _, lines := child(t, "TestEveryAcknowledgedCommitSurvivesAKilledProcess", commitChildEnv, path)
	acknowledged := 0
	for acknowledged < 40 {
		line := await(t, lines, "committed ")
		n, err := strconv.Atoi(strings.TrimPrefix(line, "committed "))
		if err != nil {
			t.Fatal(err)
		}
		acknowledged = n
	}
	// SIGKILL: no deferred function, no Close, no checkpoint runs in the child.
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("the child exited normally, want killed")
	}
	again := open(t, path)
	var missing int
	err := again.Read(deadline(t, 5*time.Second), func(tx Tx) error {
		return tx.QueryRow("SELECT ? - COUNT(*) FROM rows WHERE n BETWEEN 1 AND ?", acknowledged, acknowledged).Scan(&missing)
	})
	if err != nil {
		t.Fatal(err)
	}
	if missing != 0 {
		t.Fatalf("%d of %d acknowledged commits are missing after the kill", missing, acknowledged)
	}
	integrity(t, again)
	t.Logf("%d acknowledged commits kept, %d rows in all", acknowledged, count(t, again))
}

// commitForever is the commit child: it commits one row at a time and acknowledges each only after
// Write returned.
func commitForever(t *testing.T, path string) {
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; ; n++ {
		err := s.Write(context.Background(), func(tx Tx) error {
			_, err := tx.Exec("INSERT INTO rows(n) VALUES (?)", n)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("committed", n)
	}
}
