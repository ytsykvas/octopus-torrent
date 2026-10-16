package swarm

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/anacrolix/torrent"
)

// IncompleteError reports a torrent that did not complete: how far it got and which pieces it lacks.
// It wraps the reason, such as the context's error.
type IncompleteError struct {
	Completed, Length int64
	MissingPieces     []int
	Err               error
}

func (e *IncompleteError) Error() string {
	return fmt.Sprintf("%d of %d bytes complete, missing pieces %v: %v", e.Completed, e.Length, e.MissingPieces, e.Err)
}

func (e *IncompleteError) Unwrap() error {
	return e.Err
}

// WaitInfo waits until the torrent has its metainfo or ctx ends.
func WaitInfo(ctx context.Context, t *torrent.Torrent) error {
	select {
	case <-t.GotInfo():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("no metainfo: %w", ctx.Err())
	}
}

// WaitComplete waits until every piece is complete or ctx ends; then it returns an *IncompleteError.
func WaitComplete(ctx context.Context, t *torrent.Torrent) error {
	if err := WaitInfo(ctx, t); err != nil {
		return err
	}
	select {
	case <-t.Complete().On():
		return nil
	case <-ctx.Done():
		return &IncompleteError{Completed: t.BytesCompleted(), Length: t.Length(), MissingPieces: missingPieces(t), Err: ctx.Err()}
	}
}

// pieceBusy reports a piece the engine is still checking: queued, hashing, or storing the result.
// Tests replace it to reach the deadline branch of settle.
var pieceBusy = func(t *torrent.Torrent, piece int) bool {
	s := t.PieceState(piece)
	return s.QueuedForHash || s.Hashing || s.Marking
}

// settle waits until no piece is busy. VerifyDataContext returns as soon as a piece is hashed,
// before the engine stores whether it passed, so the completion read right after it can be stale.
func settle(ctx context.Context, t *torrent.Torrent) error {
	for slices.ContainsFunc(pieceIndexes(t), func(i int) bool { return pieceBusy(t, i) }) {
		select {
		case <-ctx.Done():
			return fmt.Errorf("pieces still being checked: %w", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	return nil
}

func pieceIndexes(t *torrent.Torrent) []int {
	indexes := make([]int, t.NumPieces())
	for i := range indexes {
		indexes[i] = i
	}
	return indexes
}

func missingPieces(t *torrent.Torrent) []int {
	var missing []int
	for i := range t.NumPieces() {
		if !t.PieceState(i).Complete {
			missing = append(missing, i)
		}
	}
	return missing
}
