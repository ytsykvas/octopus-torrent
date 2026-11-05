package library

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"octopustorrent/internal/app/contracts"
)

// Torrent is one torrent of the library with its files, as stored.
type Torrent struct {
	ID contracts.TorrentID
	// InfohashV1 and InfohashV2 are the full infohashes; at least one is set, and each set one is
	// the hash of Info (SHA-1 and SHA-256 of the same bytes, also for a hybrid).
	InfohashV1 *[20]byte
	InfohashV2 *[32]byte
	// Name is the torrent's name, as the info dictionary gives it.
	Name string
	// Info is the info dictionary the import verified.
	Info   []byte
	Source contracts.ImportSource
	// SourceMetainfo is the imported .torrent and SourceMagnet the imported magnet link; empty when
	// the import had none (stored as NULL).
	SourceMetainfo []byte
	SourceMagnet   string
	// SavePath is the absolute folder of the payload.
	SavePath     string
	ManualPaused bool
	// AddedAt is when the import was confirmed; read back in UTC.
	AddedAt time.Time
	// Files are the torrent's files in the order of the info dictionary: a file's index is its
	// position.
	Files []TorrentFile
}

// TorrentFile is one file of a torrent: its path, its size and whether the owner wants it. Path is
// relative to the torrent's SavePath, "/"-separated and clean, so it never names a file outside that
// folder: the torrent's own folder, if it has one, is its first element.
type TorrentFile struct {
	Path   string
	Size   int64
	Wanted bool
}

// check returns ErrInvalid for a record the library must not store.
func (t Torrent) check() error {
	switch {
	case !contracts.IsUUID(string(t.ID)):
		return invalid("torrent ID %q is not a UUID", t.ID)
	case t.InfohashV1 == nil && t.InfohashV2 == nil:
		return invalid("torrent %s has no infohash", t.ID)
	case len(t.Info) == 0:
		return invalid("torrent %s has no info dictionary", t.ID)
	// The engine would accept info of another hash (finding 11); a record keeps the pair it checked.
	case t.InfohashV1 != nil && sha1.Sum(t.Info) != *t.InfohashV1:
		return invalid("torrent %s has a v1 infohash of other info", t.ID)
	case t.InfohashV2 != nil && sha256.Sum256(t.Info) != *t.InfohashV2:
		return invalid("torrent %s has a v2 infohash of other info", t.ID)
	case !t.Source.Valid():
		return invalid("torrent %s has the source %q", t.ID, t.Source)
	case contracts.CheckFolderPath("savePath", t.SavePath) != nil:
		return invalid("torrent %s has the save path %q, not a clean absolute folder", t.ID, t.SavePath)
	case len(t.Files) == 0:
		return invalid("torrent %s has no files", t.ID)
	case !storable(t.AddedAt):
		return invalid("torrent %s was added at %v, which the library cannot store", t.ID, t.AddedAt)
	}
	paths := make(map[string]bool, len(t.Files))
	for i, f := range t.Files {
		switch {
		// SQLite's length() stops at a NUL, so the path's CHECK would see it as empty.
		case f.Path == "" || strings.ContainsRune(f.Path, 0):
			return invalid("file %d of torrent %s has the path %q", i, t.ID, f.Path)
		// A removal acts on these paths (031): one that is absolute, leaves the folder or is not
		// clean would name another file.
		case !filepath.IsLocal(f.Path) || path.Clean(f.Path) != f.Path || f.Path == ".":
			return invalid("file %d of torrent %s has the path %q, not a clean path inside its folder", i, t.ID, f.Path)
		case paths[f.Path]:
			return invalid("file %d of torrent %s repeats the path %q", i, t.ID, f.Path)
		case f.Size < 0:
			return invalid("file %d of torrent %s has the size %d", i, t.ID, f.Size)
		}
		paths[f.Path] = true
	}
	return nil
}

// hashArgs are the infohashes as BLOB arguments, NULL for an absent one.
func (t Torrent) hashArgs() (v1, v2 any) {
	if t.InfohashV1 != nil {
		v1 = t.InfohashV1[:]
	}
	if t.InfohashV2 != nil {
		v2 = t.InfohashV2[:]
	}
	return v1, v2
}

// AddTorrent stores a confirmed import in one transaction: the record with its identities, its
// files with their selection, and the operation that imports it (running) in the journal. A record
// with one of its infohashes is ErrDuplicate naming that record; an operation ID the journal already
// has is ErrOperationExists. Nothing is written then, or on any other error.
func (l *Library) AddTorrent(ctx context.Context, t Torrent, op OperationIntent) error {
	if err := t.check(); err != nil {
		return err
	}
	if err := op.check(); err != nil {
		return err
	}
	v1, v2 := t.hashArgs()
	insert := []statement{{
		`INSERT INTO torrents(id, infohash_v1, infohash_v2, name, info, source, source_metainfo, source_magnet,
			save_path, manual_paused, added_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{t.ID, v1, v2, t.Name, t.Info, t.Source, nullable(t.SourceMetainfo),
			nullable(t.SourceMagnet), t.SavePath, t.ManualPaused, t.AddedAt.UnixNano()},
	}}
	for i, f := range t.Files {
		insert = append(insert, statement{
			"INSERT INTO torrent_files(torrent_id, file_index, path, size, wanted) VALUES (?, ?, ?, ?, ?)",
			[]any{t.ID, i, f.Path, f.Size, f.Wanted},
		})
	}
	insert = append(insert, op.inserts(t.ID)...)
	fingerprint := contracts.Fingerprint(op.Request)
	return l.store.Write(ctx, func(tx txn) error {
		// Under the write lock (BEGIN IMMEDIATE), so no other writer, of this store or another, can
		// add the same torrent or operation between this lookup and the inserts.
		var byV1, byV2, stored sql.NullString
		if err := tx.QueryRow(`SELECT (SELECT id FROM torrents WHERE infohash_v1 = ?), (SELECT id FROM torrents WHERE infohash_v2 = ?),
			(SELECT fingerprint FROM operations WHERE operation_id = ?)`, v1, v2, op.ID).Scan(&byV1, &byV2, &stored); err != nil {
			return err
		}
		switch {
		case stored.Valid:
			return &OperationExistsError{ID: op.ID, SameRequest: stored.String == fingerprint}
		case byV1.Valid:
			return &DuplicateError{contracts.TorrentID(byV1.String)}
		case byV2.Valid:
			return &DuplicateError{contracts.TorrentID(byV2.String)}
		}
		return execAll(tx, insert)
	})
}

// Torrents returns every record with its files, from one snapshot, in the order they were added. It
// is the full read for the start (029): every record's info dictionary and imported source included.
// A file row of no record, or a gap in a torrent's file indexes, is an error: a file's index is its
// position in Files.
func (l *Library) Torrents(ctx context.Context) ([]Torrent, error) {
	var all []Torrent
	err := l.store.Read(ctx, func(tx txn) error {
		position := map[contracts.TorrentID]int{}
		if err := queryEach(tx, func(rows *sql.Rows) error {
			var t Torrent
			var v1, v2 []byte
			var magnet sql.NullString
			var added int64
			if err := rows.Scan(&t.ID, &v1, &v2, &t.Name, &t.Info, &t.Source, &t.SourceMetainfo, &magnet,
				&t.SavePath, &t.ManualPaused, &added); err != nil {
				return err
			}
			// The schema checks the lengths; a file edited by hand must not make a read panic.
			if len(v1) != 0 && len(v1) != 20 || len(v2) != 0 && len(v2) != 32 {
				return fmt.Errorf("library: torrent %s has infohashes of %d and %d bytes", t.ID, len(v1), len(v2))
			}
			if v1 != nil {
				t.InfohashV1 = (*[20]byte)(v1)
			}
			if v2 != nil {
				t.InfohashV2 = (*[32]byte)(v2)
			}
			t.SourceMagnet, t.AddedAt = magnet.String, instant(added)
			position[t.ID] = len(all)
			all = append(all, t)
			return nil
		}, `SELECT id, infohash_v1, infohash_v2, name, info, source, source_metainfo, source_magnet, save_path,
			manual_paused, added_at FROM torrents ORDER BY added_at, id`); err != nil {
			return err
		}
		return queryEach(tx, func(rows *sql.Rows) error {
			var id contracts.TorrentID
			var index int
			var f TorrentFile
			if err := rows.Scan(&id, &index, &f.Path, &f.Size, &f.Wanted); err != nil {
				return err
			}
			// The library's own writes leave neither (a foreign key; contiguous inserts), but a file
			// edited by hand must not attach a file to another torrent or shift an index.
			i, known := position[id]
			switch {
			case !known:
				return fmt.Errorf("library: a file of torrent %s, which the library lacks", id)
			case index != len(all[i].Files):
				return fmt.Errorf("library: torrent %s has file %d where file %d is next", id, index, len(all[i].Files))
			}
			all[i].Files = append(all[i].Files, f)
			return nil
		}, "SELECT torrent_id, file_index, path, size, wanted FROM torrent_files ORDER BY torrent_id, file_index")
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// SetManualPaused stores the owner's pause of the torrent (TR-01).
func (l *Library) SetManualPaused(ctx context.Context, id contracts.TorrentID, paused bool) error {
	if !contracts.IsUUID(string(id)) {
		return invalid("torrent ID %q is not a UUID", id)
	}
	return l.store.Write(ctx, func(tx txn) error {
		result, err := tx.Exec("UPDATE torrents SET manual_paused = ? WHERE id = ?", paused, id)
		if err != nil {
			return err
		}
		// modernc's RowsAffected never fails (result.go).
		if n, _ := result.RowsAffected(); n == 0 {
			return &NotFoundError{id}
		}
		return nil
	})
}

// SetFilesWanted selects or skips files of the torrent by their indexes (TR-06, TR-07) in one
// change; a repeated index counts once. An index the torrent lacks is ErrInvalid and changes no
// file: the service checks the indexes against the torrent first.
func (l *Library) SetFilesWanted(ctx context.Context, id contracts.TorrentID, indexes []int, wanted bool) error {
	switch {
	case !contracts.IsUUID(string(id)):
		return invalid("torrent ID %q is not a UUID", id)
	case len(indexes) == 0:
		return invalid("no file index for torrent %s", id)
	case slices.Min(indexes) < 0:
		return invalid("a negative file index for torrent %s", id)
	}
	unique := slices.Compact(slices.Sorted(slices.Values(indexes)))
	// One JSON array, not one parameter per index: SQLite binds at most 32766 parameters.
	list, _ := json.Marshal(unique) // a slice of ints always encodes
	return l.store.Write(ctx, func(tx txn) error {
		result, err := tx.Exec("UPDATE torrent_files SET wanted = ? WHERE torrent_id = ? AND file_index IN (SELECT value FROM json_each(?))",
			wanted, id, string(list))
		if err != nil {
			return err
		}
		// modernc's RowsAffected never fails (result.go).
		if n, _ := result.RowsAffected(); int(n) == len(unique) {
			return nil
		}
		// Fewer files changed than asked: the error rolls the change back.
		var known bool
		if err := tx.QueryRow("SELECT EXISTS (SELECT 1 FROM torrents WHERE id = ?)", id).Scan(&known); err != nil {
			return err
		}
		if !known {
			return &NotFoundError{id}
		}
		return invalid("torrent %s lacks some of the %d file indexes", id, len(unique))
	})
}
