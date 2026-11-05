-- Library schema 1 (task 014, docs/decisions/library-schema-and-data-folder.md). A released
-- migration never changes: a change is the next numbered file. Every table is STRICT. Unsigned
-- 64-bit values (inode, device, speed limits) are stored as their int64 bit pattern; times are Unix
-- nanoseconds, UTC. A NULL passes a CHECK, so "x IS NULL OR" is written only for readability.

-- One row per torrent in the library. id is the record's UUID (lower case, Go validates it); the
-- hashes are the full v1 (SHA-1) and v2 (SHA-256) infohashes, at least one of them; info is the
-- verified info dictionary; source_metainfo and source_magnet are what the import read.
CREATE TABLE torrents (
	id TEXT NOT NULL PRIMARY KEY CHECK (length(id) = 36 AND id = lower(id)),
	infohash_v1 BLOB UNIQUE CHECK (infohash_v1 IS NULL OR length(infohash_v1) = 20),
	infohash_v2 BLOB UNIQUE CHECK (infohash_v2 IS NULL OR length(infohash_v2) = 32),
	name TEXT NOT NULL,
	info BLOB NOT NULL CHECK (length(info) > 0),
	source TEXT NOT NULL CHECK (source IN ('file', 'finder', 'drop', 'magnet', 'url', 'watchFolder')),
	source_metainfo BLOB,
	source_magnet TEXT,
	save_path TEXT NOT NULL CHECK (length(save_path) > 0),
	manual_paused INTEGER NOT NULL CHECK (manual_paused IN (0, 1)),
	added_at INTEGER NOT NULL,
	CHECK (infohash_v1 IS NOT NULL OR infohash_v2 IS NOT NULL)
) STRICT;

-- The torrent's files by their index in the info dictionary: the path inside the torrent's layout
-- and whether the owner wants it.
CREATE TABLE torrent_files (
	torrent_id TEXT NOT NULL REFERENCES torrents(id) ON DELETE CASCADE,
	file_index INTEGER NOT NULL CHECK (file_index >= 0),
	path TEXT NOT NULL CHECK (length(path) > 0),
	size INTEGER NOT NULL CHECK (size >= 0),
	wanted INTEGER NOT NULL CHECK (wanted IN (0, 1)),
	PRIMARY KEY (torrent_id, file_index),
	UNIQUE (torrent_id, path)
) STRICT, WITHOUT ROWID;

-- What the last run left for the next one (files.Record). clean = 0 is the "not clean" marker that
-- is written before the first payload write; no row means the payload never ran.
CREATE TABLE payload_records (
	torrent_id TEXT NOT NULL PRIMARY KEY REFERENCES torrents(id) ON DELETE CASCADE,
	clean INTEGER NOT NULL CHECK (clean IN (0, 1)),
	file_system TEXT,
	device INTEGER,
	CHECK (clean = 0 OR (file_system IS NOT NULL AND device IS NOT NULL))
) STRICT, WITHOUT ROWID;

-- The files of a record with their fingerprints (files.FileState). A file the torrent does not have
-- cannot be recorded.
CREATE TABLE payload_files (
	torrent_id TEXT NOT NULL REFERENCES payload_records(torrent_id) ON DELETE CASCADE,
	file_index INTEGER NOT NULL,
	present INTEGER NOT NULL CHECK (present IN (0, 1)),
	size INTEGER NOT NULL CHECK (size >= 0),
	mtime INTEGER NOT NULL,
	ctime INTEGER NOT NULL,
	inode INTEGER NOT NULL,
	device INTEGER NOT NULL,
	regular INTEGER NOT NULL CHECK (regular IN (0, 1)),
	PRIMARY KEY (torrent_id, file_index),
	FOREIGN KEY (torrent_id, file_index) REFERENCES torrent_files(torrent_id, file_index) ON DELETE CASCADE
) STRICT, WITHOUT ROWID;

-- The owner's settings: one row, written by the first save; no row means the defaults.
CREATE TABLE settings (
	id INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
	default_save_path TEXT CHECK (default_save_path IS NULL OR length(default_save_path) > 0),
	theme TEXT NOT NULL CHECK (theme IN ('system', 'light', 'dark')),
	language TEXT NOT NULL CHECK (language IN ('en', 'uk', 'es', 'de', 'fr', 'pt')),
	download_limit INTEGER CHECK (download_limit IS NULL OR download_limit <> 0),
	upload_limit INTEGER CHECK (upload_limit IS NULL OR upload_limit <> 0),
	global_upload_disabled INTEGER NOT NULL CHECK (global_upload_disabled IN (0, 1))
) STRICT;

-- The journal of non-trivial operations: what was accepted, by which run, and how it ended. It
-- outlives the torrents it names, for recovery and for a repeated operationID. kind is checked
-- against the contract's vocabulary in Go, not here: later tasks add kinds without a rebuild.
CREATE TABLE operations (
	operation_id TEXT NOT NULL PRIMARY KEY CHECK (length(operation_id) = 36 AND operation_id = lower(operation_id)),
	runtime_id TEXT NOT NULL CHECK (length(runtime_id) = 36 AND runtime_id = lower(runtime_id)),
	kind TEXT NOT NULL CHECK (kind <> ''),
	fingerprint TEXT NOT NULL CHECK (length(fingerprint) > 0),
	request TEXT NOT NULL CHECK (json_valid(request)),
	state TEXT NOT NULL CHECK (state IN ('accepted', 'running', 'succeeded', 'failed', 'cancelled')),
	error TEXT CHECK (error IS NULL OR json_valid(error)),
	accepted_at INTEGER NOT NULL,
	finished_at INTEGER,
	CHECK ((state IN ('succeeded', 'failed', 'cancelled')) = (finished_at IS NOT NULL)),
	CHECK ((state IN ('failed', 'cancelled')) = (error IS NOT NULL))
) STRICT;

-- The torrents an operation names, with each one's outcome. No foreign key to torrents: deleting a
-- torrent keeps the journal.
CREATE TABLE operation_torrents (
	operation_id TEXT NOT NULL REFERENCES operations(operation_id) ON DELETE CASCADE,
	torrent_id TEXT NOT NULL CHECK (length(torrent_id) = 36 AND torrent_id = lower(torrent_id)),
	outcome TEXT CHECK (outcome IS NULL OR json_valid(outcome)),
	PRIMARY KEY (operation_id, torrent_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX operation_torrents_by_torrent ON operation_torrents(torrent_id);
