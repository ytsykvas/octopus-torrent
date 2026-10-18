package torrentengine_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/testutil/fixture"
	"octopustorrent/internal/torrentengine/testutil/swarm"
	"octopustorrent/internal/torrentengine/transferswitch"
)

// Bounds measured in task 010 (report, stage A3): measured maximum × 1.5, rounded up.
const (
	// residualBound is what a receiver may still get from one connection after the switch: the chunks
	// the sender had already written, at most its request window (measured 1008 KiB).
	residualBound = 1536 << 10
	// limitedDrain is how long that takes when the sender is limited to 256 KiB/s (measured 296 ms).
	limitedDrain = 500 * time.Millisecond
	// resumeBound is how soon upload moves again after AllowDataUpload (measured 15 ms; a lost
	// writer wake-up may add one keep-alive interval).
	resumeBound = transferswitch.WakeInterval + 250*time.Millisecond
	// holdWindow is how long a stopped direction is watched for any payload.
	holdWindow = 2 * time.Second
)

// drainWithin is how long the residual of an unlimited sender takes to reach a receiver limited to
// rate bytes per second (measured 3.9 s at 256 KiB/s, 0.97 s at 1 MiB/s).
func drainWithin(rate int) time.Duration {
	return time.Duration(residualBound)*time.Second/time.Duration(rate) + 250*time.Millisecond
}

// woken is a peer that uses the product's keep-alive interval (transferswitch.WakeInterval).
func woken(rateLimit int) swarm.Options {
	return swarm.Options{RateLimit: rateLimit, KeepAliveTimeout: transferswitch.WakeInterval}
}

// After StopUpload, the peer A of a chain S → A → L hands no payload to L, L stops receiving once
// the bytes already in flight drained, and A keeps downloading from S.
func TestStoppingUploadKeepsTheDownloadGoing(t *testing.T) {
	fx := named(t, "v1-long")
	mi := metaInfo(t, fx)
	s := seed(t, fx, woken(512<<10))
	a, l := peer(t, woken(0)), peer(t, woken(256<<10))
	at, err := a.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	lt, err := l.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(at, s)
	swarm.Connect(lt, a)
	waitFor(t, transferLimit, "the leecher behind the stopping peer got 512 KiB", func() bool { return swarm.Count(lt).PayloadRead >= 512<<10 })

	rec := swarm.Record(10*time.Millisecond, map[string]*torrent.Torrent{"a": at, "l": lt})
	t.Cleanup(func() { rec.Stop() })
	rec.Mark("accepted")
	c, err := transferswitch.StopUpload(context.Background(), at, transferswitch.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	rec.Mark("confirmed")
	drain := drainWithin(256 << 10)
	time.Sleep(drain + holdWindow)
	log := rec.Stop()

	afterConfirmed, err := log.Delta("confirmed", "a")
	if err != nil {
		t.Fatal(err)
	}
	received, err := log.Delta("accepted", "l")
	if err != nil {
		t.Fatal(err)
	}
	last, moved := log.Last("l", swarm.PayloadRead)
	if !moved {
		t.Fatal("the leecher's counter never changed while it was recorded")
	}
	drained := last - log.MarkAt("accepted")
	t.Logf("confirmed %v after the switch; the leecher got %d bytes after the command, the last %v after it; the stopping peer downloaded %d bytes meanwhile",
		c.Confirmed.Sub(c.Applied), received.PayloadRead, drained, afterConfirmed.PayloadRead)
	if c.Handed != 0 || afterConfirmed.PayloadWritten != 0 {
		t.Errorf("the stopping peer handed %d payload bytes to connections after the switch and %d after the confirmation", c.Handed, afterConfirmed.PayloadWritten)
	}
	if received.PayloadRead > residualBound || drained > drain {
		t.Errorf("the leecher got %d bytes after the command, the last %v after it; bounds %d bytes, %v", received.PayloadRead, drained, residualBound, drain)
	}
	if afterConfirmed.PayloadRead < 256<<10 {
		t.Errorf("the stopping peer downloaded only %d bytes in %v after the stop: the download did not continue", afterConfirmed.PayloadRead, drain+holdWindow)
	}
}

// Five stops and starts in one transfer from a sender that fills its buffers faster than the
// receiver reads: every stop holds within the measured residual, every start moves again.
func TestRepeatedStopsWithDataInTheBufferEndWithinTheMeasuredResidual(t *testing.T) {
	fx := named(t, "v1-long")
	s := seed(t, fx, woken(0))
	lt, err := peer(t, woken(1<<20)).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	st := sameTorrent(t, s, lt)
	swarm.Connect(lt, s)
	for cycle := range 5 {
		from := swarm.Count(lt).PayloadRead
		waitFor(t, transferLimit, "the transfer moved 256 KiB", func() bool { return swarm.Count(lt).PayloadRead >= from+256<<10 })
		atCommand := swarm.Count(lt).PayloadRead
		c, err := transferswitch.StopUpload(context.Background(), st, transferswitch.DefaultLimits())
		if err != nil {
			t.Fatalf("cycle %d: %v", cycle, err)
		}
		if c.Handed != 0 {
			t.Errorf("cycle %d: %d payload bytes handed to the connection after the switch", cycle, c.Handed)
		}
		held := holds(t, lt, swarm.PayloadRead, drainWithin(1<<20))
		if residual := held - atCommand; residual > residualBound {
			t.Errorf("cycle %d: %d bytes arrived after the command, bound %d", cycle, residual, residualBound)
		}
		resumed := time.Now()
		st.AllowDataUpload()
		waitFor(t, resumeBound, "upload moved again after the start", func() bool { return swarm.Count(lt).PayloadRead > held })
		t.Logf("cycle %d: %d bytes after the command; moving again %v after the start", cycle, held-atCommand, time.Since(resumed))
	}
}

// A connection made while upload is stopped receives no payload until upload is allowed. The
// stopped torrent still downloads, so it accepts the connection.
func TestANewConnectionGetsNoPayloadWhileUploadIsStopped(t *testing.T) {
	fx := named(t, "v1-large")
	mi := metaInfo(t, fx)
	s := seed(t, fx, woken(256<<10))
	a := peer(t, woken(0))
	at, err := a.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(at, s)
	if err := firstPayload(deadline(t, transferLimit), at); err != nil {
		t.Fatal(err)
	}
	if _, err := transferswitch.StopUpload(context.Background(), at, transferswitch.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	late, err := peer(t, woken(0)).Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(late, a)
	waitFor(t, transferLimit, "the late peer connected to the downloading torrent", func() bool { return late.Stats().ActivePeers > 0 })
	time.Sleep(holdWindow)
	if got := swarm.Count(late).PayloadRead; got != 0 {
		t.Fatalf("the connection made during the stop received %d payload bytes", got)
	}
	at.AllowDataUpload()
	if err := firstPayload(deadline(t, transferLimit), late); err != nil {
		t.Fatalf("upload did not move after it was allowed: %v", err)
	}
}

// A complete torrent whose upload is stopped neither seeds nor wants peers: the pinned engine allows
// a new connection only to a torrent that needs data or seeds (newConnsAllowed), so a leecher's
// connection is refused. After AllowDataUpload it is accepted and the leecher completes.
func TestACompleteTorrentAcceptsNoConnectionWhileUploadIsStopped(t *testing.T) {
	fx := named(t, "v1-large")
	s := seed(t, fx, woken(0))
	if _, err := transferswitch.StopUpload(context.Background(), mustTorrent(t, s, fx), transferswitch.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	lt, err := peer(t, woken(0)).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	refusedThenServed(t, lt, s, sameTorrent(t, s, lt))
}

// A torrent added while upload is stopped (the switch set before its info, as the product does for
// new imports and restored entries) uploads nothing from its first connection. A restored complete
// entry accepts no connection at all; a new import downloads and accepts the leecher, but sends it
// nothing.
func TestATorrentAddedUnderTheStopUploadsNothing(t *testing.T) {
	fx := named(t, "v1-large")
	mi := metaInfo(t, fx)
	t.Run("restored entry with its data", func(t *testing.T) {
		a := peer(t, woken(0))
		if err := fx.WritePayload(a.Dir); err != nil {
			t.Fatal(err)
		}
		at := mustAddStopped(t, a, mi)
		if err := at.VerifyDataContext(deadline(t, transferLimit)); err != nil {
			t.Fatal(err)
		}
		if err := swarm.Settle(deadline(t, transferLimit), at); err != nil {
			t.Fatal(err)
		}
		lt, err := peer(t, woken(0)).Leech(mi)
		if err != nil {
			t.Fatal(err)
		}
		refusedThenServed(t, lt, a, at)
	})
	t.Run("new import without data", func(t *testing.T) {
		a := peer(t, woken(0))
		at := mustAddStopped(t, a, mi)
		swarm.Connect(at, seed(t, fx, woken(256<<10)))
		lt, err := peer(t, woken(0)).Leech(mi)
		if err != nil {
			t.Fatal(err)
		}
		swarm.Connect(lt, a)
		waitFor(t, transferLimit, "the leecher connected to the downloading import", func() bool { return lt.Stats().ActivePeers > 0 })
		time.Sleep(holdWindow)
		if got := swarm.Count(at).PayloadWritten; got != 0 {
			t.Fatalf("the import added under the stop uploaded %d payload bytes", got)
		}
		if swarm.Count(at).PayloadRead == 0 {
			t.Fatal("the new import did not download while only upload was stopped")
		}
		at.AllowDataUpload()
		if err := firstPayload(deadline(t, transferLimit), lt); err != nil {
			t.Fatalf("upload did not move after it was allowed: %v", err)
		}
	})
}

// A torrent that completes while upload is stopped does not seed until upload is allowed.
func TestACompletedTorrentDoesNotSeedWhileUploadIsStopped(t *testing.T) {
	fx := named(t, "v1-large")
	mi := metaInfo(t, fx)
	s := seed(t, fx, woken(512<<10))
	a := peer(t, woken(0))
	at, err := a.Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(at, s)
	if err := firstPayload(deadline(t, transferLimit), at); err != nil {
		t.Fatal(err)
	}
	if _, err := transferswitch.StopUpload(context.Background(), at, transferswitch.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err := swarm.WaitComplete(deadline(t, transferLimit), at); err != nil {
		t.Fatal(err)
	}
	lt, err := peer(t, woken(0)).Leech(mi)
	if err != nil {
		t.Fatal(err)
	}
	refusedThenServed(t, lt, a, at)
}

// refusedThenServed connects the leecher to the complete stopped torrent of p, requires that no
// connection is made and no payload moves for holdWindow, then allows upload, connects again and
// requires the leecher to complete.
func refusedThenServed(t *testing.T, lt *torrent.Torrent, p *swarm.Peer, stopped *torrent.Torrent) {
	t.Helper()
	swarm.Connect(lt, p)
	time.Sleep(holdWindow)
	if got, peers := swarm.Count(lt).PayloadRead, lt.Stats().ActivePeers; got != 0 || peers != 0 {
		t.Fatalf("during the stop the leecher got %d payload bytes over %d connection(s)", got, peers)
	}
	stopped.AllowDataUpload()
	swarm.Connect(lt, p)
	if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
}

// The upload switch never touches the download switch: a manually paused torrent (both directions
// disallowed) downloads nothing through the global stop and even through an AllowDataUpload call.
// That AllowDataUpload does allow its upload; never calling it for a paused torrent is the policy's
// rule (013, 053), not the engine's.
func TestTheUploadSwitchNeverChangesTheDownloadSwitch(t *testing.T) {
	fx := named(t, "v1-large")
	s := seed(t, fx, woken(256<<10))
	pt, err := peer(t, woken(0)).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(pt, s)
	if err := firstPayload(deadline(t, transferLimit), pt); err != nil {
		t.Fatal(err)
	}
	pt.DisallowDataDownload()
	pt.DisallowDataUpload()
	paused := holds(t, pt, swarm.PayloadRead, limitedDrain)
	if _, err := transferswitch.StopUpload(context.Background(), pt, transferswitch.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	pt.AllowDataUpload()
	time.Sleep(holdWindow)
	if got := swarm.Count(pt).PayloadRead; got != paused {
		t.Fatalf("the paused torrent downloaded %d more bytes after the upload switch changed", got-paused)
	}
}

// One client, four torrents, commands from goroutines at once while the global stop is turned on:
// the stop on a torrent that downloads and uploads, a pause, an add under the stop and a resume
// under the stop (its nudge under its own lock, reading the stop). Each torrent ends in the state
// its own commands give it, shown by its payload in each direction.
func TestConcurrentCommandsKeepEachTorrentsSwitches(t *testing.T) {
	long, large, tree, single := named(t, "v1-long"), named(t, "v1-large"), named(t, "v1-tree-no-empty"), named(t, "v1-single")
	s := peer(t, woken(512<<10))
	for _, fx := range []fixture.Fixture{long, large, tree, single} {
		if err := fx.WritePayload(s.Dir); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Seed(deadline(t, transferLimit), metaInfo(t, fx)); err != nil {
			t.Fatal(err)
		}
	}
	c, l := peer(t, woken(0)), peer(t, woken(256<<10))
	leech := func(p *swarm.Peer, fx fixture.Fixture) *torrent.Torrent {
		tor, err := p.Leech(metaInfo(t, fx))
		if err != nil {
			t.Fatal(err)
		}
		return tor
	}
	chained, paused := leech(c, long), leech(c, large)
	swarm.Connect(chained, s)
	swarm.Connect(paused, s)
	resumed := mustAddStopped(t, c, metaInfo(t, single))
	resumed.DisallowDataDownload()
	swarm.Connect(resumed, s)
	behind := leech(l, long)
	swarm.Connect(behind, c)
	waitFor(t, transferLimit, "both transfers moved", func() bool {
		return swarm.Count(behind).PayloadRead >= 256<<10 && swarm.Count(paused).PayloadRead > 0
	})

	var added *torrent.Torrent
	treeInfo := metaInfo(t, tree)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	var stop transferswitch.Confirmation
	wg.Go(func() {
		var err error
		stop, err = transferswitch.StopUpload(context.Background(), chained, transferswitch.DefaultLimits())
		errs <- err
	})
	wg.Go(func() {
		paused.DisallowDataDownload()
		paused.DisallowDataUpload()
	})
	wg.Go(func() {
		var err error
		added, err = addStopped(c, treeInfo)
		errs <- err
	})
	resumeCtx := deadline(t, transferLimit)
	var resumedLock sync.Mutex // the per-torrent command lock of the caller
	wg.Go(func() {
		_, err := transferswitch.ResumeDownload(resumeCtx, resumed, func() {
			resumedLock.Lock()
			defer resumedLock.Unlock()
			transferswitch.Nudge(resumed, false) // the global stop is on
		}, transferswitch.DefaultLimits())
		errs <- err
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	swarm.Connect(added, s)
	swarm.Connect(leech(l, tree), c)
	swarm.Connect(leech(l, single), c)
	chainedWritten := swarm.Count(chained).PayloadWritten

	pausedAt := holds(t, paused, swarm.PayloadRead, limitedDrain)
	behindAt := holds(t, behind, swarm.PayloadRead, drainWithin(256<<10))
	chainedFrom := swarm.Count(chained).PayloadRead
	for _, tor := range []*torrent.Torrent{added, resumed} {
		if err := swarm.WaitComplete(deadline(t, transferLimit), tor); err != nil {
			t.Fatalf("%s did not download under the upload stop: %v", tor.Name(), err)
		}
	}
	time.Sleep(holdWindow)
	if got := swarm.Count(paused).PayloadRead; got != pausedAt {
		t.Errorf("the paused torrent downloaded %d bytes", got-pausedAt)
	}
	if got := swarm.Count(behind).PayloadRead; got != behindAt {
		t.Errorf("the leecher behind the stopped torrent got %d more bytes", got-behindAt)
	}
	if swarm.Count(chained).PayloadRead == chainedFrom {
		t.Error("the stopped torrent no longer downloads")
	}
	if got := swarm.Count(chained).PayloadWritten; stop.Handed != 0 || got != chainedWritten {
		t.Errorf("the stopped torrent handed %d payload bytes after the switch and %d after the confirmation", stop.Handed, got-chainedWritten)
	}
	for _, tor := range []*torrent.Torrent{paused, added, resumed} {
		if got := swarm.Count(tor).PayloadWritten; got != 0 {
			t.Errorf("%s uploaded %d payload bytes under the stop", tor.Name(), got)
		}
	}
}

// The cause of finding 12: once the last cancelled request is answered with a Reject, the pinned
// engine makes no new request for the update that Reject triggers, and nothing asks again — the
// keep-alive interval does not help. An update for any other reason moves it.
func TestPinnedEngineRequestsNothingAfterTheLastCancelIsRejected(t *testing.T) {
	fx := named(t, "v1-large")
	s := seed(t, fx, woken(256<<10))
	lt, err := peer(t, woken(256<<10)).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, s)
	if err := firstPayload(deadline(t, transferLimit), lt); err != nil {
		t.Fatal(err)
	}
	lt.DisallowDataDownload()
	lt.AllowDataDownload()
	var incomplete *swarm.IncompleteError
	if err := swarm.WaitComplete(deadline(t, 6*time.Second), lt); !errors.As(err, &incomplete) {
		t.Fatalf("WaitComplete = %v; the immediate resume completed with a %v keep-alive, so the engine no longer stalls: re-check 010, 018 and 025's notes", err, transferswitch.WakeInterval)
	}
	lt.DisallowDataUpload()
	lt.AllowDataUpload()
	if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
		t.Fatalf("an update for another reason did not move the stalled transfer: re-check 010's cause: %v", err)
	}
}

// The remedy for finding 12: ResumeDownload right after a pause completes the transfer, with the
// upload switch allowed again by the nudge or left stopped.
func TestResumeDownloadEndsTheStallOfAnImmediateResume(t *testing.T) {
	for _, uploadAllowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "upload allowed", false: "upload stopped"}[uploadAllowed], func(t *testing.T) {
			fx := named(t, "v1-large")
			s := seed(t, fx, woken(256<<10))
			lt, err := peer(t, woken(256<<10)).Leech(metaInfo(t, fx))
			if err != nil {
				t.Fatal(err)
			}
			if !uploadAllowed {
				lt.DisallowDataUpload()
			}
			swarm.Connect(lt, s)
			if err := firstPayload(deadline(t, transferLimit), lt); err != nil {
				t.Fatal(err)
			}
			lt.DisallowDataDownload()
			nudges, err := transferswitch.ResumeDownload(deadline(t, transferLimit), lt, func() { transferswitch.Nudge(lt, uploadAllowed) }, transferswitch.DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			if err := swarm.WaitComplete(deadline(t, transferLimit), lt); err != nil {
				t.Fatal(err)
			}
			t.Logf("completed after %d nudge(s)", nudges)
		})
	}
}

// A pause that lands while an unlimited sender's residual is still on its way: the Rejects of the
// cancelled requests arrive after those chunks. A resume right after the pause must not take the
// residual for new payload and leave the transfer in the finding-12 stall.
func TestResumeDownloadAfterAPauseWithAResidualInFlightKeepsMoving(t *testing.T) {
	fx := named(t, "v1-long")
	s := seed(t, fx, woken(0))
	lt, err := peer(t, woken(256<<10)).Leech(metaInfo(t, fx))
	if err != nil {
		t.Fatal(err)
	}
	swarm.Connect(lt, s)
	waitFor(t, transferLimit, "the leecher got 1 MiB", func() bool { return swarm.Count(lt).PayloadRead >= 1<<20 })
	lt.DisallowDataDownload()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int)
	go func() {
		n, _ := transferswitch.ResumeDownload(ctx, lt, func() { transferswitch.Nudge(lt, true) }, transferswitch.DefaultLimits())
		done <- n
	}()
	from := swarm.Count(lt).PayloadRead
	time.Sleep(drainWithin(256 << 10))
	waitFor(t, 2*holdWindow+8*time.Second, "the transfer moved 2 MiB past the residual after the resume", func() bool {
		return swarm.Count(lt).PayloadRead >= from+2<<20
	})
	cancel()
	t.Logf("kept moving after %d nudge(s)", <-done)
}

// addStopped adds a torrent the way the product adds new imports and restored entries under the
// global upload stop: the switch is set before the torrent has its info, so it never has a piece
// to send without it.
func addStopped(p *swarm.Peer, mi *metainfo.MetaInfo) (*torrent.Torrent, error) {
	tor := p.AddInfohash(mi.HashInfoBytes())
	tor.DisallowDataUpload()
	if err := tor.SetInfoBytes(mi.InfoBytes); err != nil {
		return nil, err
	}
	tor.DownloadAll()
	return tor, nil
}

func mustAddStopped(t *testing.T, p *swarm.Peer, mi *metainfo.MetaInfo) *torrent.Torrent {
	t.Helper()
	tor, err := addStopped(p, mi)
	if err != nil {
		t.Fatal(err)
	}
	return tor
}

// mustTorrent is the peer's torrent of the fixture.
func mustTorrent(t *testing.T, p *swarm.Peer, fx fixture.Fixture) *torrent.Torrent {
	t.Helper()
	tor, ok := p.Client().Torrent(metaInfo(t, fx).HashInfoBytes())
	if !ok {
		t.Fatalf("the peer in %s has no torrent %s", p.Dir, fx.Name)
	}
	return tor
}

// sameTorrent is the peer's torrent with the infohash of tor.
func sameTorrent(t *testing.T, p *swarm.Peer, tor *torrent.Torrent) *torrent.Torrent {
	t.Helper()
	other, ok := p.Client().Torrent(tor.InfoHash())
	if !ok {
		t.Fatalf("the peer in %s has no torrent %s", p.Dir, tor.InfoHash())
	}
	return other
}

// waitFor polls cond until it holds or the limit passes.
func waitFor(t *testing.T, limit time.Duration, what string, cond func() bool) {
	t.Helper()
	for ctx := deadline(t, limit); !cond(); {
		select {
		case <-ctx.Done():
			t.Fatalf("not within %v: %s", limit, what)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// holds waits for the bytes already in flight (at most drain), then requires the field to stay
// unchanged for half a second, and returns its value.
func holds(t *testing.T, tor *torrent.Torrent, f swarm.Field, drain time.Duration) int64 {
	t.Helper()
	time.Sleep(drain)
	before := swarm.Count(tor).Get(f)
	time.Sleep(500 * time.Millisecond)
	if after := swarm.Count(tor).Get(f); after != before {
		t.Fatalf("%s still moved %d bytes %v after the command", tor.Name(), after-before, drain)
	}
	return before
}
