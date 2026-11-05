package library

import (
	"database/sql"
	"encoding/json"
	"time"

	"octopustorrent/internal/app/contracts"
)

// The repositories (task 015) are the methods of Library. Each is one transaction over values the
// caller prepared in memory: none takes a callback, so no caller can wait for the engine or the
// network while a transaction is open. Each validates every value before SQLite's own checks see
// it (a constraint error is then a bug, never a user's path), and returns nil only after a durable
// commit. These are the helpers they share; each keeps an error branch in one place.

// statement is one SQL statement with its arguments.
type statement struct {
	query string
	args  []any
}

// execAll runs the statements in order and stops at the first error. Each distinct query is
// prepared once per call, so a torrent of many files is one statement executed per file.
func execAll(tx txn, statements []statement) error {
	prepared := map[string]*sql.Stmt{}
	defer func() {
		for _, stmt := range prepared {
			_ = stmt.Close() // the transaction's end releases them too; nothing to report
		}
	}()
	for _, s := range statements {
		stmt, found := prepared[s.query]
		if !found {
			var err error
			if stmt, err = tx.tx.PrepareContext(tx.ctx, s.query); err != nil {
				return err
			}
			prepared[s.query] = stmt
		}
		if _, err := stmt.ExecContext(tx.ctx, s.args...); err != nil {
			return err
		}
	}
	return nil
}

// queryEach runs a query and calls scan for every row, in order.
func queryEach(tx txn, scan func(*sql.Rows) error, query string, args ...any) error {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// storable reports whether t can be stored as Unix nanoseconds and read back as the same instant:
// years 1678 to 2262.
func storable(t time.Time) bool {
	return !t.IsZero() && time.Unix(0, t.UnixNano()).Equal(t)
}

// instant is a stored time, in UTC.
func instant(nanoseconds int64) time.Time { return time.Unix(0, nanoseconds).UTC() }

// nullable is nil (SQL NULL) for an empty value. modernc binds an empty, non-nil []byte as a
// zero-length BLOB, not as NULL (conn.go, bindBlob).
func nullable[T string | []byte](v T) any {
	if len(v) == 0 {
		return nil
	}
	return v
}

// problemJSON is the text the journal stores for an AppError: its code, IDs and parameters, never
// its cause (which may name local paths or carry SQLite's messages). Nil is SQL NULL.
func problemJSON(p *contracts.AppError) any {
	if p == nil {
		return nil
	}
	stored := contracts.AppError{Code: p.Code, OperationID: p.OperationID, TorrentID: p.TorrentID, Params: p.Params}
	if stored.Params == nil {
		stored.Params = map[string]string{}
	}
	text, _ := json.Marshal(stored) // strings, two string pointers and a map of strings always encode
	return string(text)
}
