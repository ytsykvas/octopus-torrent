package pendingmagnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/filestore"
)

var (
	// ErrInvalidWait is a request without a positive wait: no wait for metadata may be endless.
	ErrInvalidWait = errors.New("the wait for metadata must be positive")
	// ErrInvalidSource is a request without an infohash.
	ErrInvalidSource = errors.New("the request has no infohash")
	// ErrDuplicate is an infohash the client already has: a library torrent or another import holds
	// it, and this import must neither drop nor change it.
	ErrDuplicate = errors.New("the client already has this torrent")
	// ErrClientClosed is an engine client that is closed or closes during the call.
	ErrClientClosed = errors.New("the engine client is closed")
	// ErrTorrentClosed is a pending torrent that another holder dropped.
	ErrTorrentClosed = errors.New("the pending torrent was closed")
	// ErrNoMetadata is no metadata within the request's wait; it wraps context.DeadlineExceeded.
	ErrNoMetadata = errors.New("no metadata within the wait")
)

// Request is a magnet to fetch: its v1 infohash, the peers to ask, and how long to wait.
type Request struct {
	InfoHash metainfo.Hash
	Peers    []torrent.PeerInfo
	Wait     time.Duration
}

// Fetch adds the magnet to the client with a deferred storage and both directions disallowed, waits
// for its metadata and verifies it. It returns a Pending import that owns the torrent, or an error
// after which the client holds nothing of it. A request that cannot work is refused before the
// engine is touched. When the caller's context ends, the result is its error, also when the metadata
// arrived at the same moment: a cancelled import never yields metadata.
func Fetch(ctx context.Context, cl *torrent.Client, req Request) (*Pending, error) {
	switch {
	case req.Wait <= 0:
		return nil, fmt.Errorf("%w: %v", ErrInvalidWait, req.Wait)
	case req.InfoHash == metainfo.Hash{}:
		return nil, ErrInvalidSource
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := filestore.NewDeferred()
	t, isNew := cl.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: req.InfoHash, Storage: store, DisableInitialPieceCheck: true})
	if !isNew {
		return nil, ErrDuplicate
	}
	// The engine adds a torrent to a closed client and never closes it (finding 32).
	if isClosed(cl.Closed()) {
		t.Drop()
		return nil, ErrClientClosed
	}
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	t.AddPeers(req.Peers)

	limit := time.NewTimer(req.Wait)
	defer limit.Stop()
	timedOut := false
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
	case <-cl.Closed():
	case <-t.Closed():
	case <-limit.C:
		timedOut = true
	}
	err := ended(ctx, cl, t)
	// The limit and the metadata may be ready in the same instant; metadata that arrived is used.
	if err == nil && timedOut && !isClosed(t.GotInfo()) {
		err = fmt.Errorf("%w (%v): %w", ErrNoMetadata, req.Wait, context.DeadlineExceeded)
	}
	var info metainfo.Info
	var infoBytes []byte
	if err == nil {
		infoBytes = t.Metainfo().InfoBytes
		info, err = verify(req.InfoHash, infoBytes, t.Info())
	}
	if err != nil {
		t.Drop()
		return nil, err
	}
	return &Pending{client: cl, torrent: t, storage: store, info: info, infoBytes: infoBytes}, nil
}

// ended reports why an import can go no further, in a fixed order: the caller's cancel first, then a
// closed client, then a torrent closed by another holder.
func ended(ctx context.Context, cl *torrent.Client, t *torrent.Torrent) error {
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case isClosed(cl.Closed()):
		return ErrClientClosed
	case isClosed(t.Closed()):
		return ErrTorrentClosed
	}
	return nil
}

func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
