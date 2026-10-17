package filestore

import "sync"

// Session collects the storage problems of one run of the application. A run with any problem is not
// a clean quit: the next start verifies the data instead of trusting its marks (the rehash rule).
type Session struct {
	mu       sync.Mutex
	problems []error
}

// Fail records a storage problem.
func (s *Session) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = append(s.problems, err)
}

// Problems returns a copy of the recorded problems, oldest first.
func (s *Session) Problems() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.problems...)
}

// Clean reports whether no problem was recorded.
func (s *Session) Clean() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.problems) == 0
}
