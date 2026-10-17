package library

import (
	"context"
	"database/sql"
	"errors"
)

// SQLiteVersion reports the version of the SQLite compiled into this binary. It opens an in-memory
// database, so it touches no file.
func SQLiteVersion(ctx context.Context) (string, error) {
	db := sql.OpenDB(connector{":memory:"})
	var version string
	err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	return version, errors.Join(classify(err), db.Close())
}
