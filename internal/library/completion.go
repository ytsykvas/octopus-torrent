package library

import (
	"context"
	_ "embed" // the migrations are embedded
	"fmt"
)

//go:embed migrations/completion/0001_piece_completion.sql
var completionV1 string

// completionSchema is the piece-completion store's: application_id "OTPC", one migration per file of
// migrations/completion, in order.
var completionSchema = schema{name: "piece-completion", applicationID: 0x4F545043, migrations: []string{completionV1}}

// Mark is the verified state of one piece of one torrent: complete means its hash matched.
type Mark struct {
	InfoHash [20]byte
	Piece    int
	Complete bool
}

// CompletionStore keeps the engine's per-piece completion marks in its own database file, apart
// from the library. Marks are advisory: they never decide on their own that data is correct (task
// 009). Its schema follows the migration policy of task 014.
type CompletionStore struct {
	store *Store
}

// OpenCompletion opens, creates or migrates the store at an absolute, clean path. A file that is not
// a piece-completion store, or one of a newer version, is refused unchanged (ErrUnknownSchema,
// ErrUnsupportedVersion).
func OpenCompletion(ctx context.Context, path string) (*CompletionStore, error) {
	s, err := open(ctx, path, defaults, completionSchema)
	if err != nil {
		return nil, err
	}
	return &CompletionStore{s}, nil
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
		return classify(ctx.Err())
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
