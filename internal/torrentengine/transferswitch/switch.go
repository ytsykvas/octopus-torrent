package transferswitch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anacrolix/torrent"
)

// WakeInterval is the ClientConfig.KeepAliveTimeout every client uses. The pinned engine can miss a
// writer wake-up (a chansync broadcast with no waiter); its keep-alive timer is the only thing that
// runs the writer again, and only on a connection useful to either side. With the default minute a
// transfer stood still in 11 of 300 fresh runs; with 1 s in none, the longest gap 1.01 s.
const WakeInterval = time.Second

// Limits time the confirmation and the resume.
type Limits struct {
	// Poll is how often the counters are read.
	Poll time.Duration
	// Quiet is how long a counter must stay unchanged: the sent counter to confirm a stop, the
	// received counter before download is allowed again.
	Quiet time.Duration
	// Deadline is how long StopUpload waits for a quiet window before it reports ErrNotConfirmed.
	Deadline time.Duration
	// Nudge is how long ResumeDownload waits for payload before its first nudge; each further nudge
	// without payload doubles the wait, up to MaxNudge.
	Nudge, MaxNudge time.Duration
}

// DefaultLimits are the values measured in task 010.
func DefaultLimits() Limits {
	return Limits{Poll: 25 * time.Millisecond, Quiet: 250 * time.Millisecond, Deadline: 2 * time.Second, Nudge: time.Second, MaxNudge: 16 * time.Second}
}

func (l Limits) validate() error {
	if l.Poll <= 0 || l.Quiet <= 0 || l.Nudge <= 0 || l.Deadline <= l.Quiet || l.MaxNudge < l.Nudge {
		return fmt.Errorf("%w: %+v", ErrInvalidLimits, l)
	}
	return nil
}

var (
	// ErrNotConfirmed means the torrent kept handing payload to its connections after upload was
	// disallowed, until the deadline. Upload stays disallowed.
	ErrNotConfirmed = errors.New("transferswitch: upload was not confirmed as stopped")
	// ErrInvalidLimits means the limits cannot work: a period that is not positive, a deadline that
	// leaves no room for a quiet window, or a nudge cap below the first nudge.
	ErrInvalidLimits = errors.New("transferswitch: invalid limits")
)

// Uploader is the part of an engine torrent that StopUpload uses.
type Uploader interface {
	DisallowDataUpload()
	Stats() torrent.TorrentStats
}

// Switch is the part of an engine torrent that Nudge uses.
type Switch interface {
	DisallowDataUpload()
	AllowDataUpload()
}

// Downloader is the part of an engine torrent that ResumeDownload uses.
type Downloader interface {
	AllowDataDownload()
	BytesMissing() int64
	Stats() torrent.TorrentStats
}

var (
	_ Uploader   = (*torrent.Torrent)(nil)
	_ Switch     = (*torrent.Torrent)(nil)
	_ Downloader = (*torrent.Torrent)(nil)
)

// Confirmation is the timeline of a stop: when the command was accepted, when the switch returned
// and when the quiet window confirmed it; Handed is the payload handed to connections after the
// switch returned (zero on the pinned engine).
type Confirmation struct {
	Accepted, Applied, Confirmed time.Time
	Handed                       int64
}

// StopUpload disallows upload and confirms the stop. After DisallowDataUpload returns the engine
// hands no new chunk to any connection, so the torrent's sent-payload counter, which counts a chunk
// when it is buffered, stops at once. Confirmed means that counter stayed unchanged for a quiet
// window; a change restarts the window, and no full window before the deadline is ErrNotConfirmed.
// Every outcome leaves upload disallowed. Bytes already buffered or held by the kernel are still
// delivered at the receiver's pace, and protocol messages (metadata, haves) are not payload.
func StopUpload(ctx context.Context, t Uploader, lim Limits) (Confirmation, error) {
	if err := lim.validate(); err != nil {
		return Confirmation{}, err
	}
	c := Confirmation{Accepted: time.Now()}
	t.DisallowDataUpload()
	c.Applied = time.Now()
	base := sent(t)
	last, quietSince := base, c.Applied
	deadline := time.NewTimer(lim.Deadline)
	defer deadline.Stop()
	tick := time.NewTicker(lim.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return c, ctx.Err()
		case <-deadline.C:
			return c, fmt.Errorf("%w: %d payload bytes handed to connections within %v after the switch", ErrNotConfirmed, sent(t)-base, lim.Deadline)
		case <-tick.C:
			n, now := sent(t), time.Now()
			if n != last {
				last, quietSince = n, now
			} else if now.Sub(quietSince) >= lim.Quiet {
				c.Confirmed, c.Handed = now, last-base
				return c, nil
			}
		}
	}
}

// ResumeDownload allows download again after a pause and keeps the transfer out of the pinned
// engine's stall (finding 12): the pause cancelled every request, and the last Reject of those
// requests can leave the torrent asking for nothing, also after the chunks already in flight have
// arrived. So it does not stop at the first byte: until nothing is missing or the context ends, it
// calls nudge whenever no payload arrived for the current wait, which starts at Nudge, doubles with
// each nudge up to MaxNudge and resets when payload arrives. It returns the number of nudges, nil
// when nothing is missing, or the context's error (canceled before the allow, download stays
// disallowed; after it, allowed).
//
// The caller runs it for as long as the torrent is not paused, under the torrent's context, and runs
// nudge under its per-torrent command lock, reading the upload policy at that moment
// (Nudge(t, uploadAllowed)), so a global stop or a pause applied meanwhile is never undone.
func ResumeDownload(ctx context.Context, t Downloader, nudge func(), lim Limits) (nudges int, err error) {
	if err := lim.validate(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	t.AllowDataDownload()
	tick := time.NewTicker(lim.Poll)
	defer tick.Stop()
	last, lastMove, wait := received(t), time.Now(), lim.Nudge
	for t.BytesMissing() > 0 {
		select {
		case <-ctx.Done():
			return nudges, ctx.Err()
		case <-tick.C:
			n, now := received(t), time.Now()
			if n != last {
				last, lastMove, wait = n, now, lim.Nudge
			} else if now.Sub(lastMove) >= wait {
				nudge()
				nudges++
				lastMove, wait = now, min(2*wait, lim.MaxNudge)
			}
		}
	}
	return nudges, nil
}

// Nudge makes the torrent update its requests for a reason other than a Reject:
// DisallowDataUpload always does, and AllowDataUpload follows only when the policy allows upload
// now. It is the nudge ResumeDownload needs; run it under the caller's per-torrent lock.
func Nudge(t Switch, uploadAllowed bool) {
	t.DisallowDataUpload()
	if uploadAllowed {
		t.AllowDataUpload()
	}
}

func sent(t Uploader) int64 {
	s := t.Stats()
	return s.BytesWrittenData.Int64()
}

func received(t Downloader) int64 {
	s := t.Stats()
	return s.BytesReadData.Int64()
}
