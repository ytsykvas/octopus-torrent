package library

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// schema is what a database file of one purpose holds: the application_id that marks the file as
// ours and the list of migrations. Migration i brings the database from version i to version i+1
// (PRAGMA user_version), so the current version is the number of migrations. A released migration
// never changes; a change is a new migration at the end.
type schema struct {
	name          string
	applicationID int32
	migrations    []string
}

// current is the version the migrations bring a database to.
func (s schema) current() int { return len(s.migrations) }

// script is the SQL that brings a database from version from to the current one, stamping it as
// ours on the way. SQLite keeps both pragmas in the transaction that runs it.
func (s schema) script(from int) string {
	parts := append([]string{fmt.Sprintf("PRAGMA application_id = %d", s.applicationID)}, s.migrations[from:]...)
	return strings.Join(append(parts, fmt.Sprintf("PRAGMA user_version = %d", s.current())), ";\n")
}

// fileState is what decides whether a file is ours and at which version: its application_id, its
// user_version, the number of rows of sqlite_schema and a description of its structure.
type fileState struct {
	applicationID int32
	version       int
	objects       int
	structure     string
}

// structureQuery describes the tables, columns, foreign keys, indexes, triggers and views of the
// main database, one sorted line each, from the pragmas whose output is stable across SQLite
// versions rather than from the stored SQL text. SQLite's own tables (sqlite_stat1 after ANALYZE,
// sqlite_sequence) are not part of a schema. CHECK constraints are visible only as SQL text and are
// not compared.
const structureQuery = `WITH t AS (
		SELECT name, type, wr, strict FROM pragma_table_list
		WHERE schema = 'main' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'),
	i AS (SELECT t.name AS tbl, l.* FROM t, pragma_index_list(t.name) AS l),
	line(s) AS (
		SELECT printf('table %s %s wr=%d strict=%d', name, type, wr, strict) FROM t
		UNION ALL
		SELECT printf('column %s %d %s %s notnull=%d default=%s pk=%d hidden=%d',
			t.name, c.cid, c.name, c.type, c."notnull", quote(c.dflt_value), c.pk, c.hidden)
		FROM t, pragma_table_xinfo(t.name) AS c
		UNION ALL
		SELECT printf('foreign key %s %d.%d %s(%s) -> %s on update %s on delete %s match %s',
			t.name, f.id, f.seq, f."from", f."table", quote(f."to"), f.on_update, f.on_delete, f."match")
		FROM t, pragma_foreign_key_list(t.name) AS f
		UNION ALL
		SELECT printf('index %s %s unique=%d origin=%s partial=%d', tbl, name, "unique", origin, partial) FROM i
		UNION ALL
		SELECT printf('index column %s %d %d %s desc=%d %s key=%d', i.name, x.seqno, x.cid, quote(x.name), x."desc", x.coll, x."key")
		FROM i, pragma_index_xinfo(i.name) AS x
		UNION ALL
		SELECT printf('%s %s %s', type, name, sql) FROM sqlite_schema WHERE type IN ('trigger', 'view'))
	SELECT COALESCE(string_agg(s, char(10) ORDER BY s), '') FROM line`

// inspect reads the file's state in one query.
func inspect(tx txn) (fileState, error) {
	var st fileState
	err := tx.QueryRow(`SELECT (SELECT application_id FROM pragma_application_id),
		(SELECT user_version FROM pragma_user_version),
		(SELECT COUNT(*) FROM sqlite_schema),
		(`+structureQuery+`)`).Scan(&st.applicationID, &st.version, &st.objects, &st.structure)
	return st, err
}

// reference is the structure that migrations 1..v of sch build: they run on a private in-memory
// database. An error is a migration that does not build, never a refusal of the file.
func reference(ctx context.Context, sch schema, v int) (string, error) {
	db := sql.OpenDB(connector{"file::memory:?_pragma=" + foreignKeysOff})
	defer db.Close()
	var st fileState
	err := run(ctx, db, nil, func(tx txn) error {
		_, err := tx.Exec(strings.Join(sch.migrations[:v], ";\n"))
		if err == nil {
			st, err = inspect(tx)
		}
		return err
	})
	return st.structure, err
}

// examine decides, inside tx, whether the file is a database of sch and at which version:
//
//   - a new file (no application_id, version 0, nothing in sqlite_schema) is at version 0;
//   - ours at a version this build knows, with the structure the migrations build, is at that version;
//   - ours at a newer version is ErrUnsupportedVersion: a newer build wrote it;
//   - anything else is ErrUnknownSchema: another application's file, an unstamped file with tables,
//     or ours with another structure.
func examine(tx txn, sch schema) (int, error) {
	st, err := inspect(tx)
	if err != nil {
		return 0, err
	}
	if st == (fileState{}) {
		return 0, nil
	}
	ours := st.applicationID == sch.applicationID
	if ours && st.version > sch.current() {
		return 0, fmt.Errorf("%w: the %s database is at version %d, this build reads versions up to %d",
			ErrUnsupportedVersion, sch.name, st.version, sch.current())
	}
	matches := false
	if ours && st.version > 0 {
		want, err := reference(tx.ctx, sch, st.version)
		if err != nil {
			return 0, fmt.Errorf("the migrations of the %s database up to version %d: %w", sch.name, st.version, err)
		}
		matches = st.structure == want
	}
	if !matches {
		return 0, fmt.Errorf("%w: application_id %#x, user_version %d, %d schema objects; want a %s database (application_id %#x) of the structure its migrations build",
			ErrUnknownSchema, st.applicationID, st.version, st.objects, sch.name, sch.applicationID)
	}
	return st.version, nil
}

// migrate brings the database to the current version of sch inside tx, a write transaction (BEGIN
// IMMEDIATE): the version again under the write lock (another store may have migrated meanwhile),
// every pending migration with the new application_id and version, and a foreign key check. Any
// error rolls everything back: the old version, the old application_id and every row.
//
// Foreign keys are off on the connection that runs it: SQLite ignores the setting inside a
// transaction, and with them on, rebuilding a table (new table, copy, drop, rename) would delete the
// rows that refer to it through ON DELETE CASCADE. The check before COMMIT refuses a migration that
// leaves a reference broken.
func migrate(tx txn, sch schema) error {
	from, err := examine(tx, sch)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(sch.script(from)); err != nil {
		return err
	}
	var broken int
	err = tx.QueryRow("SELECT COUNT(*) FROM pragma_foreign_key_check").Scan(&broken)
	if err == nil && broken > 0 {
		err = fmt.Errorf("%d rows refer to a missing row through a foreign key", broken)
	}
	return err
}
