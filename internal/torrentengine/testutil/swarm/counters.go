package swarm

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

// Counters are one torrent's byte counters: payload (piece data) separately from everything sent
// and received, protocol messages included.
type Counters struct {
	PayloadRead, PayloadWritten, Read, Written int64
}

// Count reads the torrent's counters now.
func Count(t *torrent.Torrent) Counters {
	s := t.Stats()
	return Counters{PayloadRead: s.BytesReadData.Int64(), PayloadWritten: s.BytesWrittenData.Int64(), Read: s.BytesRead.Int64(), Written: s.BytesWritten.Int64()}
}

// Field names one payload counter, so the log functions can follow one direction of traffic: a
// peer that downloads and uploads at once moves both.
type Field int

const (
	// Payload is the piece data read and written together.
	Payload Field = iota
	// PayloadRead is the piece data received.
	PayloadRead
	// PayloadWritten is the piece data sent.
	PayloadWritten
)

// Get is the value of the field in the counters.
func (c Counters) Get(f Field) int64 {
	return [...]int64{c.PayloadRead + c.PayloadWritten, c.PayloadRead, c.PayloadWritten}[f]
}

// Sub is the growth of every counter from earlier to c.
func (c Counters) Sub(earlier Counters) Counters {
	return Counters{PayloadRead: c.PayloadRead - earlier.PayloadRead, PayloadWritten: c.PayloadWritten - earlier.PayloadWritten,
		Read: c.Read - earlier.Read, Written: c.Written - earlier.Written}
}

// Sample is the counters of every recorded torrent at one moment, measured from the recorder's start.
// A sample taken by Mark carries its label.
type Sample struct {
	At       time.Duration
	Label    string
	Counters map[string]Counters
}

// Log is what a Recorder measured.
type Log struct {
	Samples []Sample
}

// Recorder samples the counters of named torrents at an interval until it is stopped.
type Recorder struct {
	torrents map[string]*torrent.Torrent
	start    time.Time
	mu       sync.Mutex
	log      Log
	stop     chan struct{}
	done     chan struct{}
	stopped  sync.Once
}

// Record starts sampling the torrents every interval.
func Record(interval time.Duration, torrents map[string]*torrent.Torrent) *Recorder {
	r := &Recorder{torrents: maps.Clone(torrents), start: time.Now(), stop: make(chan struct{}), done: make(chan struct{})}
	r.sample("")
	go func() {
		defer close(r.done)
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				r.sample("")
			case <-r.stop:
				return
			}
		}
	}()
	return r
}

func (r *Recorder) sample(label string) {
	s := Sample{At: time.Since(r.start), Label: label, Counters: map[string]Counters{}}
	for name, t := range r.torrents {
		s.Counters[name] = Count(t)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log.Samples = append(r.log.Samples, s)
}

// Mark takes a labelled sample now, such as the moment a command was given.
func (r *Recorder) Mark(label string) {
	r.sample(label)
}

// Stop ends the sampling, takes a last sample and returns the log. Calling it again returns the
// same log.
func (r *Recorder) Stop() Log {
	r.stopped.Do(func() {
		close(r.stop)
		<-r.done
		r.sample("")
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.log
}

// First is when the field of the named torrent was first above zero, and whether it ever was.
func (l Log) First(name string, f Field) (time.Duration, bool) {
	for _, s := range l.Samples {
		if s.Counters[name].Get(f) > 0 {
			return s.At, true
		}
	}
	return 0, false
}

// Last is the time of the last sample at which the field of the named torrent had changed, and
// whether it ever changed.
func (l Log) Last(name string, f Field) (time.Duration, bool) {
	at, found := time.Duration(0), false
	for i := 1; i < len(l.Samples); i++ {
		if l.Samples[i].Counters[name].Get(f) != l.Samples[i-1].Counters[name].Get(f) {
			at, found = l.Samples[i].At, true
		}
	}
	return at, found
}

// MarkAt is when the labelled sample was taken; 0 when there is none.
func (l Log) MarkAt(label string) time.Duration {
	for _, s := range l.Samples {
		if s.Label == label {
			return s.At
		}
	}
	return 0
}

// Delta is how much every counter of the named torrent grew between the labelled sample and the
// last sample: for example the residual traffic after a stop command.
func (l Log) Delta(label, name string) (Counters, error) {
	for _, s := range l.Samples {
		if s.Label == label {
			return l.Samples[len(l.Samples)-1].Counters[name].Sub(s.Counters[name]), nil
		}
	}
	return Counters{}, fmt.Errorf("no sample labelled %q", label)
}
