package filestore

import (
	"errors"
	"sync"
	"testing"
)

func TestASessionIsCleanUntilAProblemIsReported(t *testing.T) {
	var s Session
	if !s.Clean() || len(s.Problems()) != 0 {
		t.Fatal("a new session is not clean")
	}
	first, second := errors.New("first"), errors.New("second")
	s.Fail(first)
	s.Fail(second)
	if s.Clean() {
		t.Fatal("a session with problems is clean")
	}
	got := s.Problems()
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Fatalf("Problems = %v", got)
	}
	got[0] = nil // the caller's copy must not change the session
	if s.Problems()[0] != first {
		t.Fatal("Problems returned the session's own slice")
	}
}

func TestProblemsMayBeReportedFromManyGoroutines(t *testing.T) {
	var s Session
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() { defer wg.Done(); s.Fail(errors.New("x")) }()
	}
	wg.Wait()
	if n := len(s.Problems()); n != 50 {
		t.Fatalf("%d problems, want 50", n)
	}
}
