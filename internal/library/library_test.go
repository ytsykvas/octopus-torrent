package library

import (
	"bufio"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"octopustorrent/internal/datadir"
)

const (
	idA  = "3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b"
	idB  = "8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e"
	opA  = "c0ffee00-1234-4abc-9def-0123456789ab"
	runA = "0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
)

// openLibrary opens a library in a new temporary folder and closes it after the test.
func openLibrary(t *testing.T) (*Library, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), datadir.LibraryFile)
	lib, err := OpenLibrary(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatalf("OpenLibrary: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	return lib, path
}

// base is one valid row in every table that has rows of its own.
var base = []string{
	`INSERT INTO torrents(id, infohash_v1, infohash_v2, name, info, source, save_path, manual_paused, added_at)
		VALUES ('` + idA + `', zeroblob(20), zeroblob(32), 'A', x'64', 'file', '/Users/me/Downloads', 0, 1)`,
	`INSERT INTO torrent_files(torrent_id, file_index, path, size, wanted) VALUES ('` + idA + `', 0, 'a.txt', 1, 1)`,
	`INSERT INTO payload_records(torrent_id, clean, file_system, device) VALUES ('` + idA + `', 1, 'apfs', 16777234)`,
	`INSERT INTO payload_files(torrent_id, file_index, present, size, mtime, ctime, inode, device, regular)
		VALUES ('` + idA + `', 0, 1, 1, 2, 3, 4, 16777234, 1)`,
	`INSERT INTO settings(id, default_save_path, theme, language, download_limit, upload_limit, global_upload_disabled)
		VALUES (1, NULL, 'system', 'en', NULL, NULL, 0)`,
	`INSERT INTO operations(operation_id, runtime_id, kind, fingerprint, request, state, error, accepted_at, finished_at)
		VALUES ('` + opA + `', '` + runA + `', 'pauseTorrents', 'fp', '{}', 'running', NULL, 1, NULL)`,
	`INSERT INTO operation_torrents(operation_id, torrent_id, outcome) VALUES ('` + opA + `', '` + idA + `', NULL)`,
}

// withBase opens a library holding the base rows.
func withBase(t *testing.T) (*Library, string) {
	t.Helper()
	lib, path := openLibrary(t)
	for _, statement := range base {
		must(t, lib.store, statement)
	}
	return lib, path
}

// rowCounts is the number of rows of every library table.
func rowCounts(t *testing.T, s *Store) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, table := range libraryTables {
		var n int
		if err := s.Read(deadline(t, 5*time.Second), func(tx Tx) error {
			return tx.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		counts[table] = n
	}
	return counts
}

var libraryTables = []string{"torrents", "torrent_files", "payload_records", "payload_files", "settings", "operations", "operation_torrents"}

func TestANewLibraryHasTheFirstSchemaAndItsId(t *testing.T) {
	lib, path := openLibrary(t)
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	st := stateOf(t, path)
	if st.applicationID != 0x4F544C42 || st.version != 1 {
		t.Fatalf("application_id %#x, user_version %d; want OTLB, 1", st.applicationID, st.version)
	}
	if st.structure != referenceOf(t, librarySchema, 1) {
		t.Fatal("the structure differs from the migrations'")
	}
	tables := scalar[string](t, path, "SELECT group_concat(name || '=' || strict, ' ' ORDER BY name) FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\'")
	want := []string{}
	for _, name := range slices.Sorted(slices.Values(libraryTables)) {
		want = append(want, name+"=1")
	}
	if tables != strings.Join(want, " ") {
		t.Fatalf("tables %q, want every one STRICT: %q", tables, strings.Join(want, " "))
	}
}

func TestTheLibraryEnforcesItsConstraints(t *testing.T) {
	torrent := func(id, v1, v2, rest string) string {
		return `INSERT INTO torrents(id, infohash_v1, infohash_v2, name, info, source, save_path, manual_paused, added_at) VALUES ('` +
			id + `', ` + v1 + `, ` + v2 + `, ` + rest + `)`
	}
	valid := `'B', x'64', 'magnet', '/x', 0, 1`
	operation := func(id, run, kind, fingerprint, request, state, errJSON, finished string) string {
		return `INSERT INTO operations(operation_id, runtime_id, kind, fingerprint, request, state, error, accepted_at, finished_at) VALUES ('` +
			id + `', '` + run + `', '` + kind + `', '` + fingerprint + `', '` + request + `', '` + state + `', ` + errJSON + `, 1, ` + finished + `)`
	}
	opB := "d0ffee00-1234-4abc-9def-0123456789ab"
	for name, statement := range map[string]string{
		"a duplicate v1 hash":           torrent(idB, "zeroblob(20)", "NULL", valid),
		"a duplicate v2 hash":           torrent(idB, "NULL", "zeroblob(32)", valid),
		"no hash":                       torrent(idB, "NULL", "NULL", valid),
		"a short v1 hash":               torrent(idB, "randomblob(19)", "NULL", valid),
		"a short v2 hash":               torrent(idB, "NULL", "randomblob(31)", valid),
		"a v1 hash as text":             torrent(idB, "'0123456789abcdef0123'", "NULL", valid),
		"a duplicate id":                torrent(idA, "randomblob(20)", "NULL", valid),
		"an id of the wrong length":     torrent("x", "randomblob(20)", "NULL", valid),
		"an upper-case id":              torrent(strings.ToUpper(idB), "randomblob(20)", "NULL", valid),
		"no info":                       torrent(idB, "randomblob(20)", "NULL", `'B', x'', 'magnet', '/x', 0, 1`),
		"an unknown source":             torrent(idB, "randomblob(20)", "NULL", `'B', x'64', 'ftp', '/x', 0, 1`),
		"no save path":                  torrent(idB, "randomblob(20)", "NULL", `'B', x'64', 'magnet', '', 0, 1`),
		"a pause of 2":                  torrent(idB, "randomblob(20)", "NULL", `'B', x'64', 'magnet', '/x', 2, 1`),
		"a file of a missing torrent":   `INSERT INTO torrent_files VALUES ('` + idB + `', 0, 'b', 1, 1)`,
		"a duplicate file index":        `INSERT INTO torrent_files VALUES ('` + idA + `', 0, 'other', 1, 1)`,
		"a duplicate file path":         `INSERT INTO torrent_files VALUES ('` + idA + `', 1, 'a.txt', 1, 1)`,
		"a negative file index":         `INSERT INTO torrent_files VALUES ('` + idA + `', -1, 'b', 1, 1)`,
		"an empty file path":            `INSERT INTO torrent_files VALUES ('` + idA + `', 1, '', 1, 1)`,
		"a negative file size":          `INSERT INTO torrent_files VALUES ('` + idA + `', 1, 'b', -1, 1)`,
		"wanted of 2":                   `INSERT INTO torrent_files VALUES ('` + idA + `', 1, 'b', 1, 2)`,
		"a clean record without volume": `UPDATE payload_records SET file_system = NULL`,
		"a clean flag of 2":             `UPDATE payload_records SET clean = 2`,
		"a record of a missing torrent": `INSERT INTO payload_records VALUES ('` + idB + `', 0, NULL, NULL)`,
		"a payload file of no file":     `INSERT INTO payload_files VALUES ('` + idA + `', 5, 1, 1, 2, 3, 4, 5, 1)`,
		"a payload file of no record": torrent(idB, "randomblob(20)", "NULL", valid) + `; INSERT INTO torrent_files VALUES ('` + idB +
			`', 0, 'b', 1, 1); INSERT INTO payload_files VALUES ('` + idB + `', 0, 1, 1, 2, 3, 4, 5, 1)`,
		"present of 2":                   `UPDATE payload_files SET present = 2`,
		"regular of 2":                   `UPDATE payload_files SET regular = 2`,
		"a negative payload file size":   `UPDATE payload_files SET size = -1`,
		"a second settings row":          `INSERT INTO settings VALUES (2, NULL, 'system', 'en', NULL, NULL, 0)`,
		"an unknown theme":               `UPDATE settings SET theme = 'blue'`,
		"an unknown language":            `UPDATE settings SET language = 'ru'`,
		"a download limit of 0":          `UPDATE settings SET download_limit = 0`,
		"an upload limit of 0":           `UPDATE settings SET upload_limit = 0`,
		"an empty default folder":        `UPDATE settings SET default_save_path = ''`,
		"a global stop of 2":             `UPDATE settings SET global_upload_disabled = 2`,
		"an empty operation kind":        operation(opB, runA, "", "fp", "{}", "running", "NULL", "NULL"),
		"an unknown operation state":     operation(opB, runA, "pauseTorrents", "fp", "{}", "paused", "NULL", "NULL"),
		"a bad runtime id":               operation(opB, "runtime", "pauseTorrents", "fp", "{}", "running", "NULL", "NULL"),
		"an upper-case runtime id":       operation(opB, strings.ToUpper(runA), "pauseTorrents", "fp", "{}", "running", "NULL", "NULL"),
		"a bad operation id":             operation("op", runA, "pauseTorrents", "fp", "{}", "running", "NULL", "NULL"),
		"an upper-case operation id":     operation(strings.ToUpper(opB), runA, "pauseTorrents", "fp", "{}", "running", "NULL", "NULL"),
		"a duplicate operation id":       operation(opA, runA, "pauseTorrents", "fp", "{}", "running", "NULL", "NULL"),
		"an empty fingerprint":           operation(opB, runA, "pauseTorrents", "", "{}", "running", "NULL", "NULL"),
		"a request that is not JSON":     operation(opB, runA, "pauseTorrents", "fp", "{", "running", "NULL", "NULL"),
		"a running operation finished":   operation(opB, runA, "pauseTorrents", "fp", "{}", "running", "NULL", "2"),
		"a succeeded one not finished":   operation(opB, runA, "pauseTorrents", "fp", "{}", "succeeded", "NULL", "NULL"),
		"a failed one without its error": operation(opB, runA, "pauseTorrents", "fp", "{}", "failed", "NULL", "2"),
		"a succeeded one with an error":  operation(opB, runA, "pauseTorrents", "fp", "{}", "succeeded", `'{}'`, "2"),
		"an error that is not JSON":      operation(opB, runA, "pauseTorrents", "fp", "{}", "failed", `'{'`, "2"),
		"a torrent of no operation":      `INSERT INTO operation_torrents VALUES ('` + opB + `', '` + idA + `', NULL)`,
		"a journal torrent id of 1 char": `INSERT INTO operation_torrents VALUES ('` + opA + `', 'x', NULL)`,
		"an upper-case journal torrent":  `INSERT INTO operation_torrents VALUES ('` + opA + `', '` + strings.ToUpper(idB) + `', NULL)`,
		"a torrent named twice":          `INSERT INTO operation_torrents VALUES ('` + opA + `', '` + idA + `', NULL)`,
		"an outcome that is not JSON":    `INSERT INTO operation_torrents VALUES ('` + opA + `', '` + idB + `', '{')`,
	} {
		t.Run(name, func(t *testing.T) {
			lib, _ := withBase(t)
			before := rowCounts(t, lib.store)
			err := lib.store.Write(deadline(t, 5*time.Second), func(tx Tx) error {
				_, err := tx.Exec(statement)
				return err
			})
			if !errors.Is(err, ErrConstraint) {
				t.Fatalf("%s = %v, want ErrConstraint", statement, err)
			}
			if after := rowCounts(t, lib.store); !mapsEqual(after, before) {
				t.Fatalf("rows went from %v to %v", before, after)
			}
		})
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestTwoWritersOfTheSameHashCommitExactlyOnce(t *testing.T) {
	_, path := openLibrary(t)
	// Two stores of one file, as two processes would have; they close after the test, since two
	// closing checkpoints at once may be ErrBusy.
	stores := []*Library{}
	for range 2 {
		lib, err := OpenLibrary(deadline(t, 10*time.Second), path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lib.Close() })
		stores = append(stores, lib)
	}
	errs := make(chan error, 2)
	for i, id := range []string{idA, idB} {
		go func() {
			errs <- stores[i].store.Write(deadline(t, 10*time.Second), func(tx Tx) error {
				_, err := tx.Exec(`INSERT INTO torrents(id, infohash_v1, name, info, source, save_path, manual_paused, added_at)
					VALUES (?, zeroblob(20), 'same', x'64', 'file', '/x', 0, 1)`, id)
				return err
			})
		}()
	}
	var committed, refusedOnce int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			committed++
		case errors.Is(err, ErrConstraint):
			refusedOnce++
		default:
			t.Fatalf("a concurrent insert = %v", err)
		}
	}
	if committed != 1 || refusedOnce != 1 {
		t.Fatalf("%d committed, %d refused; want exactly one of each", committed, refusedOnce)
	}
	if got := scalar[int](t, path, "SELECT COUNT(*) FROM torrents"); got != 1 {
		t.Fatalf("%d torrents, want 1", got)
	}
}

func TestDeletingATorrentKeepsTheOperationsThatNamedIt(t *testing.T) {
	lib, _ := withBase(t)
	must(t, lib.store, "DELETE FROM torrents WHERE id = ?", idA)
	got := rowCounts(t, lib.store)
	want := map[string]int{"torrents": 0, "torrent_files": 0, "payload_records": 0, "payload_files": 0, "settings": 1, "operations": 1, "operation_torrents": 1}
	if !mapsEqual(got, want) {
		t.Fatalf("after deleting the torrent the rows are %v, want %v", got, want)
	}
}

func TestEveryValueIsKeptExactly(t *testing.T) {
	lib, _ := openLibrary(t)
	added := time.Date(2026, 11, 4, 12, 30, 15, 123456789, time.UTC).UnixNano()
	info := []byte{0, 0xff, 0, 'd', 0}
	path := "Папка/файл 🎵 – «0».mkv"
	unsigned := int64(-1) // math.MaxUint64 as its int64 bit pattern
	var values = []any{idA, info, "Ім'я торента", "/Users/me/Завантаження", added, path, int64(math.MaxInt64), unsigned}
	must(t, lib.store, `INSERT INTO torrents(id, infohash_v2, name, info, source, save_path, manual_paused, added_at)
		VALUES (?, randomblob(32), ?, ?, 'watchFolder', ?, 1, ?)`, idA, values[2], info, values[3], added)
	must(t, lib.store, `INSERT INTO torrent_files VALUES (?, 0, ?, ?, 0)`, idA, path, int64(math.MaxInt64))
	must(t, lib.store, `INSERT INTO payload_records VALUES (?, 1, 'apfs', ?)`, idA, unsigned)
	must(t, lib.store, `INSERT INTO payload_files VALUES (?, 0, 1, ?, ?, ?, ?, ?, 1)`, idA, int64(math.MaxInt64), added, added+1, unsigned, unsigned)
	must(t, lib.store, `INSERT INTO settings VALUES (1, ?, 'dark', 'uk', ?, ?, 1)`, values[3], unsigned, int64(1))
	var (
		gotInfo                         []byte
		gotName, gotSave, gotPath       string
		gotAdded, gotSize, gotDev       int64
		gotMtime, gotCtime, gotInode    int64
		gotLimit                        int64
		gotPaused, gotWanted, gotGlobal bool
	)
	err := lib.store.Read(deadline(t, 5*time.Second), func(tx Tx) error {
		return tx.QueryRow(`SELECT t.info, t.name, t.save_path, t.added_at, t.manual_paused, f.path, f.size, f.wanted, r.device,
			p.mtime, p.ctime, p.inode, s.download_limit, s.global_upload_disabled
			FROM torrents t JOIN torrent_files f ON f.torrent_id = t.id JOIN payload_records r ON r.torrent_id = t.id
			JOIN payload_files p ON p.torrent_id = t.id, settings s`).Scan(
			&gotInfo, &gotName, &gotSave, &gotAdded, &gotPaused, &gotPath, &gotSize, &gotWanted, &gotDev,
			&gotMtime, &gotCtime, &gotInode, &gotLimit, &gotGlobal)
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string][2]any{
		"info": {string(gotInfo), string(info)}, "name": {gotName, values[2]}, "save path": {gotSave, values[3]},
		"added": {gotAdded, added}, "paused": {gotPaused, true}, "path": {gotPath, path}, "size": {gotSize, int64(math.MaxInt64)},
		"wanted": {gotWanted, false}, "device": {uint64(gotDev), uint64(math.MaxUint64)}, "mtime": {gotMtime, added},
		"ctime": {gotCtime, added + 1}, "inode": {uint64(gotInode), uint64(math.MaxUint64)},
		"limit": {uint64(gotLimit), uint64(math.MaxUint64)}, "global": {gotGlobal, true},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %v, want %v", name, c[0], c[1])
		}
	}
}

func TestEveryEnumerationIsTheContractsVocabulary(t *testing.T) {
	data, err := os.ReadFile("../app/contracts/testdata/vocabulary.json")
	if err != nil {
		t.Fatal(err)
	}
	var vocabulary map[string][]string
	if err := json.Unmarshal(data, &vocabulary); err != nil {
		t.Fatal(err)
	}
	_, path := openLibrary(t)
	for column, key := range map[string]string{
		"torrents.source": "importSources", "settings.theme": "themes", "settings.language": "languages",
		"operations.state": "operationStates",
	} {
		table, name, _ := strings.Cut(column, ".")
		sql := scalar[string](t, path, "SELECT sql FROM sqlite_schema WHERE name = '"+table+"'")
		match := regexp.MustCompile(`\b` + name + ` TEXT NOT NULL CHECK \(` + name + ` IN \(([^)]*)\)\)`).FindStringSubmatch(sql)
		if match == nil {
			t.Errorf("%s has no CHECK list", column)
			continue
		}
		var allowed []string
		for _, quoted := range strings.Split(match[1], ",") {
			allowed = append(allowed, strings.Trim(strings.TrimSpace(quoted), "'"))
		}
		if !slices.Equal(allowed, vocabulary[key]) {
			t.Errorf("%s allows %v, the vocabulary's %s is %v", column, allowed, key, vocabulary[key])
		}
	}
	// The operation kinds grow with later tasks (030, 056–061): the repository checks them against
	// the vocabulary in Go, and SQLite only refuses an empty kind, so a new kind needs no rebuild.
	sql := scalar[string](t, path, "SELECT sql FROM sqlite_schema WHERE name = 'operations'")
	if !strings.Contains(sql, "kind TEXT NOT NULL CHECK (kind <> '')") {
		t.Errorf("operations.kind is not checked only for being non-empty: %s", sql)
	}
}

func TestAnOperationKindOfALaterTaskNeedsNoMigration(t *testing.T) {
	lib, _ := withBase(t)
	err := lib.store.Write(deadline(t, 5*time.Second), func(tx Tx) error {
		_, err := tx.Exec(`INSERT INTO operations(operation_id, runtime_id, kind, fingerprint, request, state, error, accepted_at, finished_at)
			VALUES ('d0ffee00-1234-4abc-9def-0123456789ab', '` + runA + `', 'relocateData', 'fp', '{}', 'running', NULL, 1, NULL)`)
		return err
	})
	if err != nil {
		t.Fatalf("an operation of a kind added later = %v, want it stored", err)
	}
}

// fixtureStatements reads a frozen text fixture: its "-- application_id:" and "-- user_version:"
// header lines become PRAGMA statements, then every statement follows, each ending with ";" at the
// end of a line.
func fixtureStatements(t *testing.T, name string) []string {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "schemas", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var statements []string
	var statement strings.Builder
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		line := lines.Text()
		if pragma, value, ok := strings.Cut(strings.TrimPrefix(line, "-- "), ": "); ok && (pragma == "application_id" || pragma == "user_version") {
			n, err := strconv.ParseInt(value, 0, 64)
			if err != nil {
				t.Fatalf("%s: %v", line, err)
			}
			statements = append(statements, "PRAGMA "+pragma+" = "+strconv.FormatInt(n, 10))
			continue
		}
		if strings.HasPrefix(line, "--") || strings.TrimSpace(line) == "" {
			continue
		}
		statement.WriteString(line + "\n")
		if strings.HasSuffix(line, ";") {
			statements = append(statements, statement.String())
			statement.Reset()
		}
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	return statements
}

// fixture builds a database from a frozen text fixture and returns the number of rows it inserts
// into each table.
func fixture(t *testing.T, name, path string) map[string]int {
	t.Helper()
	statements := fixtureStatements(t, name)
	inserts := map[string]int{}
	for _, s := range statements {
		if table, ok := strings.CutPrefix(s, "INSERT INTO "); ok {
			inserts[strings.Fields(table)[0]]++
		}
	}
	foreignDB(t, path, statements...)
	return inserts
}

// The structural check leaves CHECK contents out, so this is what freezes a released migration's
// text: the schema SQLite stores after migrations 1..N must be the one frozen in library-vN.sql.
func TestTheFrozenSchemaIsWhatItsMigrationsBuild(t *testing.T) {
	var frozen []string
	for _, s := range fixtureStatements(t, "library-v1.sql") {
		if strings.HasPrefix(s, "CREATE ") {
			frozen = append(frozen, strings.TrimSuffix(s, ";\n"))
		}
	}
	path := filepath.Join(t.TempDir(), "built.sqlite")
	foreignDB(t, path, librarySchema.migrations[:1]...)
	built := scalar[string](t, path, "SELECT string_agg(sql, char(10) || '--' || char(10) ORDER BY rowid) FROM sqlite_schema WHERE sql IS NOT NULL")
	if want := strings.Join(frozen, "\n--\n"); built != want {
		t.Fatalf("migration 1 builds another schema than library-v1.sql froze:\n%s\n\nfrozen:\n%s", built, want)
	}
}

func TestTheFrozenFirstSchemaOpensAtTheCurrentVersionWithEveryRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), datadir.LibraryFile)
	inserts := fixture(t, "library-v1.sql", path)
	lib, err := OpenLibrary(deadline(t, 10*time.Second), path)
	if err != nil {
		t.Fatalf("OpenLibrary of the version 1 fixture: %v", err)
	}
	got := rowCounts(t, lib.store)
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	for _, table := range libraryTables {
		if got[table] != inserts[table] || inserts[table] == 0 {
			t.Errorf("%s holds %d rows, the fixture inserts %d (and must insert some)", table, got[table], inserts[table])
		}
	}
	if st := stateOf(t, path); st.version != librarySchema.current() {
		t.Fatalf("the fixture is at version %d, want %d", st.version, librarySchema.current())
	}
}

func TestALibraryIsNotAMarksStoreNorTheOtherWayRound(t *testing.T) {
	lib, libPath := openLibrary(t)
	if err := lib.Close(); err != nil {
		t.Fatal(err)
	}
	refused(t, libPath, completionSchema, ErrUnknownSchema)

	marksPath := filepath.Join(t.TempDir(), "piece-completion.sqlite")
	marks, err := OpenCompletion(deadline(t, 10*time.Second), marksPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := marks.Close(); err != nil {
		t.Fatal(err)
	}
	refused(t, marksPath, librarySchema, ErrUnknownSchema)
	if _, err := OpenLibrary(deadline(t, 10*time.Second), marksPath); !errors.Is(err, ErrUnknownSchema) {
		t.Fatalf("OpenLibrary of a marks store = %v, want ErrUnknownSchema", err)
	}
}

func TestTheEmbeddedMigrationsAreTheFilesInOrder(t *testing.T) {
	for dir, sch := range map[string]schema{"library": librarySchema, "completion": completionSchema} {
		entries, err := os.ReadDir(filepath.Join("migrations", dir))
		if err != nil {
			t.Fatal(err)
		}
		var files []string
		for i, e := range entries {
			if want := strconv.Itoa(i + 1); !strings.HasPrefix(e.Name(), strings.Repeat("0", 4-len(want))+want+"_") || !strings.HasSuffix(e.Name(), ".sql") {
				t.Errorf("%s/%s: want NNNN_<what>.sql, numbered from 0001 without a gap", dir, e.Name())
			}
			data, err := os.ReadFile(filepath.Join("migrations", dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, string(data))
		}
		if !slices.Equal(files, sch.migrations) {
			t.Errorf("the %s schema does not list every migration file in order", dir)
		}
	}
}
