package transferswitch

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
)

// fakeTorrent is a test double of the engine torrent: it records the switch calls and serves
// counters that the test moves. The real engine is measured in internal/torrentengine.
type fakeTorrent struct {
	mu       sync.Mutex
	calls    []string
	written  int64
	read     int64
	complete bool
	stats    int // Stats calls so far
	// growWrittenAt adds a chunk to the written counter at that Stats call (0: never).
	growWrittenAt int
	grewAt        time.Time
	// growWrittenAlways adds a byte to the written counter on every Stats call.
	growWrittenAlways bool
	// residualReads adds a byte to the read counter on each of that many Stats calls.
	residualReads int
	lastResidual  time.Time
}

func (f *fakeTorrent) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeTorrent) DisallowDataUpload() { f.record("disallow upload") }
func (f *fakeTorrent) AllowDataUpload()    { f.record("allow upload") }
func (f *fakeTorrent) AllowDataDownload()  { f.record("allow download") }

func (f *fakeTorrent) BytesMissing() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.complete {
		return 0
	}
	return 1
}

func (f *fakeTorrent) Stats() (s torrent.TorrentStats) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stats++
	if f.stats == f.growWrittenAt {
		f.written += 16 << 10
		f.grewAt = time.Now()
	}
	if f.growWrittenAlways {
		f.written++
	}
	if f.residualReads > 0 {
		f.residualReads--
		f.read++
		f.lastResidual = time.Now()
	}
	s.BytesWrittenData.Add(f.written)
	s.BytesReadData.Add(f.read)
	return s
}

func (f *fakeTorrent) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

var fast = Limits{Poll: time.Millisecond, Quiet: 20 * time.Millisecond, Deadline: 200 * time.Millisecond, Nudge: 10 * time.Millisecond, MaxNudge: 40 * time.Millisecond}

func TestStopUploadIsConfirmedAfterAQuietWindow(t *testing.T) {
	f := &fakeTorrent{}
	c, err := StopUpload(context.Background(), f, fast)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Calls(); !slices.Equal(got, []string{"disallow upload"}) {
		t.Errorf("calls %v, want only the upload switch", got)
	}
	if c.Accepted.After(c.Applied) || c.Confirmed.Sub(c.Applied) < fast.Quiet || c.Handed != 0 {
		t.Errorf("confirmation %+v: not ordered, confirmed before a quiet window, or payload handed after the switch", c)
	}
}

func TestGrowthAfterTheSwitchRestartsTheQuietWindow(t *testing.T) {
	f := &fakeTorrent{growWrittenAt: 5}
	c, err := StopUpload(context.Background(), f, fast)
	if err != nil {
		t.Fatal(err)
	}
	if c.Confirmed.Sub(f.grewAt) < fast.Quiet || c.Handed != 16<<10 {
		t.Errorf("confirmed %v after the growth, handed %d: want a full quiet window after it and the chunk counted", c.Confirmed.Sub(f.grewAt), c.Handed)
	}
}

func TestUploadThatKeepsGrowingIsNotConfirmed(t *testing.T) {
	f := &fakeTorrent{growWrittenAlways: true}
	start := time.Now()
	_, err := StopUpload(context.Background(), f, fast)
	if !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("err = %v, want ErrNotConfirmed", err)
	}
	if took := time.Since(start); took < fast.Deadline {
		t.Errorf("gave up after %v, before the deadline %v", took, fast.Deadline)
	}
}

func TestACanceledStopLeavesUploadDisallowed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeTorrent{growWrittenAlways: true}
	if _, err := StopUpload(ctx, f, fast); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := f.Calls(); !slices.Equal(got, []string{"disallow upload"}) {
		t.Errorf("calls %v: a canceled stop must still leave upload disallowed", got)
	}
}

func TestLimitsThatCannotWorkAreRefused(t *testing.T) {
	ok := fast
	for _, lim := range []Limits{{}, func() Limits { l := ok; l.Deadline = l.Quiet; return l }(), func() Limits { l := ok; l.Nudge = 0; return l }(), func() Limits { l := ok; l.MaxNudge = l.Nudge / 2; return l }()} {
		f := &fakeTorrent{}
		if _, err := StopUpload(context.Background(), f, lim); !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("StopUpload with %+v: err = %v, want ErrInvalidLimits", lim, err)
		}
		if _, err := ResumeDownload(context.Background(), f, func() {}, lim); !errors.Is(err, ErrInvalidLimits) {
			t.Errorf("ResumeDownload with %+v: err = %v, want ErrInvalidLimits", lim, err)
		}
		if got := f.Calls(); len(got) != 0 {
			t.Errorf("limits %+v: the engine was called: %v", lim, got)
		}
	}
	if err := DefaultLimits().validate(); err != nil {
		t.Errorf("the default limits are refused: %v", err)
	}
}

func (f *fakeTorrent) finish() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.complete = true
}

func TestResumeDownloadAllowsAndReturnsWhenNothingIsMissing(t *testing.T) {
	f := &fakeTorrent{complete: true}
	n, err := ResumeDownload(context.Background(), f, func() { t.Error("nudged a complete torrent") }, fast)
	if err != nil || n != 0 {
		t.Fatalf("ResumeDownload = %d, %v", n, err)
	}
	if got := f.Calls(); !slices.Equal(got, []string{"allow download"}) {
		t.Errorf("calls %v", got)
	}
}

// Payload resets the wait, so a stall that forms after the first bytes (the residual of the pause
// arriving before the last Reject) is still nudged at the first interval.
func TestResumeDownloadKeepsWatchingAfterPayloadAndNudgesAgain(t *testing.T) {
	f := &fakeTorrent{residualReads: 5}
	var at []time.Time
	n, err := ResumeDownload(context.Background(), f, func() {
		at = append(at, time.Now())
		if len(at) == 2 {
			f.finish()
		}
	}, fast)
	if err != nil || n != 2 {
		t.Fatalf("ResumeDownload = %d, %v; want 2 nudges", n, err)
	}
	if f.lastResidual.IsZero() || at[0].Sub(f.lastResidual) < fast.Nudge {
		t.Errorf("first nudge %v after the last payload, want at least %v", at[0].Sub(f.lastResidual), fast.Nudge)
	}
	if gap := at[1].Sub(at[0]); gap < 2*fast.Nudge {
		t.Errorf("second nudge %v after the first, want the doubled wait %v", gap, 2*fast.Nudge)
	}
}

func TestTheNudgeWaitStopsGrowingAtItsCap(t *testing.T) {
	f := &fakeTorrent{}
	var at []time.Time
	if _, err := ResumeDownload(context.Background(), f, func() {
		at = append(at, time.Now())
		if len(at) == 5 {
			f.finish()
		}
	}, fast); err != nil {
		t.Fatal(err)
	}
	// Waits 10, 20, 40, 40 ms: the last gap is the cap, not 80 ms.
	if gap := at[4].Sub(at[3]); gap < fast.MaxNudge || gap >= 2*fast.MaxNudge {
		t.Errorf("gap between the last nudges %v, want the cap %v", gap, fast.MaxNudge)
	}
}

func TestACanceledResumeAllowsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeTorrent{}
	if _, err := ResumeDownload(ctx, f, func() {}, fast); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if got := f.Calls(); len(got) != 0 {
		t.Errorf("a canceled resume called the engine: %v", got)
	}
}

func TestAResumeEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeTorrent{}
	n, err := ResumeDownload(ctx, f, cancel, fast)
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("ResumeDownload = %d, %v; want 1 nudge and context.Canceled", n, err)
	}
	if got := f.Calls(); !slices.Equal(got, []string{"allow download"}) {
		t.Errorf("calls %v", got)
	}
}

func TestNudgeAllowsUploadAgainOnlyWhenAllowed(t *testing.T) {
	for allowed, want := range map[bool][]string{true: {"disallow upload", "allow upload"}, false: {"disallow upload"}} {
		f := &fakeTorrent{}
		Nudge(f, allowed)
		if got := f.Calls(); !slices.Equal(got, want) {
			t.Errorf("Nudge(upload allowed %v) calls %v, want %v", allowed, got, want)
		}
	}
}

// The caller runs the nudge under its per-torrent lock and reads the policy at that moment, so a
// global stop applied during a resume is never undone by a later nudge.
func TestAStopDuringAResumeIsNotUndoneByANudge(t *testing.T) {
	f := &fakeTorrent{}
	var mu sync.Mutex
	uploadAllowed := true
	nudges := 0
	n, err := ResumeDownload(context.Background(), f, func() {
		mu.Lock()
		defer mu.Unlock()
		Nudge(f, uploadAllowed)
		nudges++
		switch nudges {
		case 1:
			// The global stop, applied between two nudges under the same lock.
			uploadAllowed = false
			go func() {
				mu.Lock()
				defer mu.Unlock()
				if _, err := StopUpload(context.Background(), f, fast); err != nil {
					t.Error(err)
				}
			}()
		case 4:
			f.finish()
		}
	}, fast)
	if err != nil || n != 4 {
		t.Fatalf("ResumeDownload = %d, %v", n, err)
	}
	calls := f.Calls()
	if last := slices.Index(calls, "allow upload"); last != 2 || slices.Contains(calls[last+1:], "allow upload") {
		t.Errorf("calls %v: upload was allowed again after the stop", calls)
	}
}
