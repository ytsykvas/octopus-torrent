package pendingmagnet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/filestore"
	"octopustorrent/internal/torrentengine/infocheck"
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

// Request is a magnet to fetch: its v1 infohash, the peers to ask, how long to wait, and what its
// metadata may be and describe.
type Request struct {
	InfoHash metainfo.Hash
	Peers    []torrent.PeerInfo
	Wait     time.Duration
	Limits   infocheck.Limits
}

// Fetch adds the magnet to the client with a deferred storage and both directions disallowed, waits
// until the gate has assembled its metadata, vets it and only then gives it to the engine. It
// returns a Pending import that owns the torrent, or an error after which the client holds nothing
// of it. A request that cannot work is refused before the engine is touched. When the caller's
// context ends, the result is its error, also when the metadata arrived at the same moment: a
// cancelled import never yields metadata. cl must have been started with the gate's configuration.
func (g *Gate) Fetch(ctx context.Context, cl *torrent.Client, req Request) (*Pending, error) {
	switch {
	case req.Wait <= 0:
		return nil, fmt.Errorf("%w: %v", ErrInvalidWait, req.Wait)
	case req.InfoHash == metainfo.Hash{}:
		return nil, ErrInvalidSource
	}
	if err := req.Limits.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store := filestore.NewDeferred(func(info *metainfo.Info) error { return infocheck.Check(info, req.Limits) })
	t, isNew := cl.AddTorrentOpt(torrent.AddTorrentOpts{InfoHash: req.InfoHash, Storage: store, DisableInitialPieceCheck: true})
	if !isNew {
		return nil, ErrDuplicate
	}
	// The engine adds a torrent to a closed client and never closes it (finding 32).
	if isClosed(cl.Closed()) {
		t.Drop()
		return nil, ErrClientClosed
	}
	metadata := g.register(t, req.Limits.MetadataBytes)
	defer g.unregister(t)
	t.DisallowDataDownload()
	t.DisallowDataUpload()
	t.AddPeers(req.Peers)

	limit := time.NewTimer(req.Wait)
	defer limit.Stop()
	timedOut := false
	select {
	case <-metadata.done:
	case <-ctx.Done():
	case <-cl.Closed():
	case <-t.Closed():
	case <-limit.C:
		timedOut = true
	}
	err := ended(ctx, cl, t)
	// The limit and the metadata may be ready in the same instant; metadata that arrived is used.
	if err == nil && timedOut && !isClosed(metadata.done) {
		err = fmt.Errorf("%w (%v): %w", ErrNoMetadata, req.Wait, context.DeadlineExceeded)
	}
	var infoBytes []byte
	var info *metainfo.Info
	if err == nil {
		infoBytes, err = metadata.result()
	}
	if err == nil {
		if info, err = vet(req.InfoHash, infoBytes, req.Limits); err != nil {
			err = metadata.refused(err)
		}
	}
	if err == nil {
		err = t.SetInfoBytes(infoBytes)
	}
	if err == nil {
		err = sameInfo(info, t.Info())
	}
	if err != nil {
		t.Drop()
		return nil, err
	}
	return &Pending{client: cl, torrent: t, storage: store, info: *info, infoBytes: infoBytes}, nil
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
