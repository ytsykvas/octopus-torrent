package pendingmagnet

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

var (
	// ErrInfoMismatch is metadata whose SHA-1 is not the requested infohash: a lying or broken peer.
	ErrInfoMismatch = errors.New("the metadata does not match the requested infohash")
	// ErrBadInfo is metadata that matches the infohash but cannot be read.
	ErrBadInfo = errors.New("the metadata cannot be read")
	// ErrInfoReplaced is verified bytes that are not the info the engine applied: another add path
	// replaced the bytes of a torrent whose info was already set (finding 31).
	ErrInfoReplaced = errors.New("the verified metadata is not the info the engine applied")
	// ErrV2Only is metadata with no v1 part; the pinned engine panics on those torrents (007).
	ErrV2Only = errors.New("v2-only metadata is not supported")
)

// verify checks received info bytes against the infohash that was requested, and against the info
// the engine applied: the engine keeps the first info it set while a later SetInfoBytes replaces only
// the bytes (finding 31), and the deferred storage opens the folder with the applied info. It never
// looks at the engine's own infohash: a btih that is a truncated v2 hash makes the engine rewrite it.
func verify(requested metainfo.Hash, infoBytes []byte, applied *metainfo.Info) (metainfo.Info, error) {
	if got := metainfo.HashBytes(infoBytes); got != requested {
		return metainfo.Info{}, fmt.Errorf("%w: requested %s, received %s", ErrInfoMismatch, requested.HexString(), got.HexString())
	}
	var info metainfo.Info
	if err := bencode.Unmarshal(infoBytes, &info); err != nil {
		return metainfo.Info{}, fmt.Errorf("%w: %w", ErrBadInfo, err)
	}
	if !info.HasV1() {
		return metainfo.Info{}, ErrV2Only
	}
	if !reflect.DeepEqual(info, *applied) {
		return metainfo.Info{}, ErrInfoReplaced
	}
	return info, nil
}
