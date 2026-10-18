package pendingmagnet

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/filestore"
)

var (
	// ErrConfirmed is a Confirm or Cancel after the import was confirmed: the torrent belongs to the
	// caller of the successful Confirm.
	ErrConfirmed = errors.New("the import is already confirmed")
	// ErrCanceled is a Confirm after the import was canceled.
	ErrCanceled = errors.New("the import is canceled")
)

// Final is what a confirmation decides: the guarded storage of the folder the user chose, whether to
// start downloading, and whether upload is allowed now (the global stop).
type Final struct {
	Storage  *filestore.TorrentStorage
	Download bool
	Upload   bool
}

type state int

const (
	waiting state = iota
	confirmed
	canceled
)

// Pending is a magnet import whose verified metadata is known and whose folder is not chosen yet. It
// owns its engine torrent until Confirm hands it over or Cancel drops it. Its methods are safe for
// concurrent use; exactly one Confirm or Cancel decides.
type Pending struct {
	mu        sync.Mutex
	state     state
	client    *torrent.Client
	torrent   *torrent.Torrent
	storage   *filestore.Deferred
	info      metainfo.Info
	infoBytes []byte
}

// Info is the verified info dictionary: the file list for the confirmation dialog. Its slices are
// shared with the import: read it, do not change it.
func (p *Pending) Info() metainfo.Info {
	return p.info
}

// InfoBytes is a copy of the verified info bytes, for the durable record.
func (p *Pending) InfoBytes() []byte {
	return slices.Clone(p.infoBytes)
}

// Touched is how often the engine tried to read, write or mark a piece before the confirmation; the
// tests prove it stays zero.
func (p *Pending) Touched() int64 {
	return p.storage.Touched()
}

// Confirm attaches the final storage and applies the switches, then hands the torrent to the caller.
// A folder that already holds a path of the torrent is refused before it is opened: every piece is
// cached as not complete, so the engine would overwrite that file (filestore.ErrPathExists). A
// storage that refuses the folder (a path that is not safe, a file system the engine cannot hash on)
// returns its error too. Either way the import keeps waiting for another folder.
func (p *Pending) Confirm(ctx context.Context, f Final) (*torrent.Torrent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.open(ctx); err != nil {
		return nil, err
	}
	if f.Storage == nil {
		return nil, filestore.ErrNoFinalStorage
	}
	if err := f.Storage.CheckNew(&p.info); err != nil {
		return nil, err
	}
	if err := p.storage.Attach(ctx, f.Storage); err != nil {
		return nil, err
	}
	if f.Upload {
		p.torrent.AllowDataUpload()
	}
	// Download was never allowed while the torrent had its metadata, so no cancelled request can
	// stall it (finding 25).
	if f.Download {
		p.torrent.AllowDataDownload()
	}
	p.state = confirmed
	return p.torrent, nil
}

// Cancel drops the pending torrent. A second Cancel does nothing; a Cancel after Confirm is
// ErrConfirmed and leaves the torrent running.
func (p *Pending) Cancel() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch p.state {
	case confirmed:
		return ErrConfirmed
	case canceled:
		return nil
	}
	p.state = canceled
	p.torrent.Drop()
	return nil
}

// open reports why the import can no longer be confirmed.
func (p *Pending) open(ctx context.Context) error {
	switch p.state {
	case confirmed:
		return ErrConfirmed
	case canceled:
		return ErrCanceled
	}
	return ended(ctx, p.client, p.torrent)
}
