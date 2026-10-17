package library

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnsupportedVersion reports a database that is not a piece-completion store of this version:
// a newer one, or one that holds other tables. It is never ErrCorrupt, so a caller that rebuilds
// a damaged store cannot destroy a library written by a newer application.
var ErrUnsupportedVersion = errors.New("the database is not a piece-completion store of a version this application reads")

// completionVersion is the PRAGMA user_version of a piece-completion store.
const completionVersion = 1

// Mark is the verified state of one piece of one torrent: complete means its hash matched.
type Mark struct {
	InfoHash [20]byte
	Piece    int
	Complete bool
}

// CompletionStore keeps the engine's per-piece completion marks in its own database file, apart
// from the library. Marks are advisory: they never decide on their own that data is correct (task
// 009). The schema precedes the migration policy of task 014.
type CompletionStore struct {
	store *Store
}

// OpenCompletion opens, or creates, the store at an absolute path with the settings of Open. A file
// that is a database of another version or purpose is refused, unchanged.
func OpenCompletion(ctx context.Context, path string) (*CompletionStore, error) {
	s, err := Open(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := s.Write(ctx, ensureCompletionSchema); err != nil {
		return nil, errors.Join(err, s.Close())
	}
	return &CompletionStore{s}, nil
}

func ensureCompletionSchema(tx Tx) error {
	var version, tables, ours int
	err := tx.QueryRow(`SELECT (SELECT user_version FROM pragma_user_version),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'),
		(SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'piece_completion')`).Scan(&version, &tables, &ours)
	empty := version == 0 && tables == 0
	if err == nil && empty {
		_, err = tx.Exec(fmt.Sprintf(`CREATE TABLE piece_completion(
			infohash BLOB NOT NULL, piece INTEGER NOT NULL, complete INTEGER NOT NULL,
			PRIMARY KEY(infohash, piece)) WITHOUT ROWID;
			PRAGMA user_version = %d`, completionVersion))
	}
	if err != nil {
		return err
	}
	if empty || version == completionVersion && tables == 1 && ours == 1 {
		return nil
	}
	return fmt.Errorf("%w: user_version %d, %d tables", ErrUnsupportedVersion, version, tables)
}

// SaveMarks writes the marks in one transaction: all of them are kept or none. A later mark of the
// same piece replaces an earlier one.
func (c *CompletionStore) SaveMarks(ctx context.Context, marks []Mark) error {
	for _, m := range marks {
		if m.Piece < 0 {
			return fmt.Errorf("library: a mark of piece %d: the piece is negative", m.Piece)
		}
	}
	if len(marks) == 0 {
		return ctx.Err()
	}
	return c.store.Write(ctx, func(tx Tx) error {
		for _, m := range marks {
			if _, err := tx.Exec(`INSERT INTO piece_completion(infohash, piece, complete) VALUES (?, ?, ?)
				ON CONFLICT(infohash, piece) DO UPDATE SET complete = excluded.complete`,
				m.InfoHash[:], m.Piece, m.Complete); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadMarks returns every saved mark of the torrent. A piece with no mark is absent from the map.
func (c *CompletionStore) LoadMarks(ctx context.Context, infoHash [20]byte) (map[int]bool, error) {
	marks := map[int]bool{}
	err := c.store.Read(ctx, func(tx Tx) error {
		rows, err := tx.Query("SELECT piece, complete FROM piece_completion WHERE infohash = ?", infoHash[:])
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var piece int
			var complete bool
			if err := rows.Scan(&piece, &complete); err != nil {
				return err
			}
			marks[piece] = complete
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return marks, nil
}

// Close checkpoints and closes the store; its error is the Store's.
func (c *CompletionStore) Close() error {
	return c.store.Close()
}
