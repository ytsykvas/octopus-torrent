package pendingmagnet

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/anacrolix/torrent/metainfo"

	"octopustorrent/internal/torrentengine/infocheck"
)

var (
	// ErrInfoMismatch is metadata whose SHA-1 is not the requested infohash, or a metadata piece that
	// cannot belong to it: a lying or broken peer.
	ErrInfoMismatch = errors.New("the metadata does not match the requested infohash")
	// ErrInfoReplaced is verified bytes that are not the info the engine applied: another add path
	// set the torrent's info first, and the engine keeps the first info it set (finding 31).
	ErrInfoReplaced = errors.New("the verified metadata is not the info the engine applied")
)

// vet checks assembled metadata before the engine sees it: its SHA-1 must be the requested infohash
// (the engine takes any info for a v1 hash, finding 11; a btih that is a truncated v2 hash is a
// mismatch), and infocheck must admit the bytes within the limits.
func vet(requested metainfo.Hash, infoBytes []byte, l infocheck.Limits) (*metainfo.Info, error) {
	if got := metainfo.HashBytes(infoBytes); got != requested {
		return nil, fmt.Errorf("%w: requested %s, received %s", ErrInfoMismatch, requested.HexString(), got.HexString())
	}
	return infocheck.CheckBytes(infoBytes, l)
}

// sameInfo checks that the engine applied the vetted info, not one another add path set first.
func sameInfo(vetted, applied *metainfo.Info) error {
	if !reflect.DeepEqual(*vetted, *applied) {
		return ErrInfoReplaced
	}
	return nil
}
