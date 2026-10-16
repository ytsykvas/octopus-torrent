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

func (c Counters) payload() int64 {
	return c.PayloadRead + c.PayloadWritten
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

// Stop ends the sampling, takes a last sample and returns the log.
func (r *Recorder) Stop() Log {
	close(r.stop)
	<-r.done
	r.sample("")
	return r.log
}

// FirstPayload is when the torrent had first moved payload, and whether it ever did.
func (l Log) FirstPayload(name string) (time.Duration, bool) {
	for _, s := range l.Samples {
		if s.Counters[name].payload() > 0 {
			return s.At, true
		}
	}
	return 0, false
}

// LastPayload is the first sample after the torrent's payload stopped changing, and whether it
// ever changed.
func (l Log) LastPayload(name string) (time.Duration, bool) {
	at, found := time.Duration(0), false
	for i := 1; i < len(l.Samples); i++ {
		if l.Samples[i].Counters[name].payload() != l.Samples[i-1].Counters[name].payload() {
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

// PayloadAfter is the payload the torrent moved between the labelled sample and the last sample:
// for example the residual traffic after a stop command.
func (l Log) PayloadAfter(label, name string) (int64, error) {
	for _, s := range l.Samples {
		if s.Label == label {
			return l.Samples[len(l.Samples)-1].Counters[name].payload() - s.Counters[name].payload(), nil
		}
	}
	return 0, fmt.Errorf("no sample labelled %q", label)
}
