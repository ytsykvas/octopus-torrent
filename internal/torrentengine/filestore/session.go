package filestore

import "sync"

// maxProblems bounds what a session keeps: the engine's background work calls the storage after it is
// closed, and each such call is a problem.
const maxProblems = 100

// Session collects the storage problems of one run of the application. A run with any problem is not
// a clean quit: the next start verifies the data instead of trusting its marks (the rehash rule).
type Session struct {
	mu       sync.Mutex
	problems []error
	dropped  int
}

// Fail records a storage problem. Beyond maxProblems only the count grows.
func (s *Session) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.problems) >= maxProblems {
		s.dropped++
		return
	}
	s.problems = append(s.problems, err)
}

// Problems returns a copy of the recorded problems, oldest first.
func (s *Session) Problems() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.problems...)
}

// Dropped is the number of problems after the first maxProblems, which were counted and not kept.
func (s *Session) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Clean reports whether no problem was recorded.
func (s *Session) Clean() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.problems) == 0 && s.dropped == 0
}
