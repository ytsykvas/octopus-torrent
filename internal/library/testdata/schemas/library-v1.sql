-- The library at schema version 1, frozen (task 014). Made once by OpenLibrary of schema 1 and
-- the rows below; never regenerated from the migrations, so a change to a released migration fails
-- TestTheFrozenSchemaIsWhatItsMigrationsBuild. See README.md.
-- application_id: 0x4F544C42
-- user_version: 1

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

CREATE TABLE torrent_files (
	torrent_id TEXT NOT NULL REFERENCES torrents(id) ON DELETE CASCADE,
	file_index INTEGER NOT NULL CHECK (file_index >= 0),
	path TEXT NOT NULL CHECK (length(path) > 0),
	size INTEGER NOT NULL CHECK (size >= 0),
	wanted INTEGER NOT NULL CHECK (wanted IN (0, 1)),
	PRIMARY KEY (torrent_id, file_index),
	UNIQUE (torrent_id, path)
) STRICT, WITHOUT ROWID;

CREATE TABLE payload_records (
	torrent_id TEXT NOT NULL PRIMARY KEY REFERENCES torrents(id) ON DELETE CASCADE,
	clean INTEGER NOT NULL CHECK (clean IN (0, 1)),
	file_system TEXT,
	device INTEGER,
	CHECK (clean = 0 OR (file_system IS NOT NULL AND device IS NOT NULL))
) STRICT, WITHOUT ROWID;

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

CREATE TABLE settings (
	id INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
	default_save_path TEXT CHECK (default_save_path IS NULL OR length(default_save_path) > 0),
	theme TEXT NOT NULL CHECK (theme IN ('system', 'light', 'dark')),
	language TEXT NOT NULL CHECK (language IN ('en', 'uk', 'es', 'de', 'fr', 'pt')),
	download_limit INTEGER CHECK (download_limit IS NULL OR download_limit <> 0),
	upload_limit INTEGER CHECK (upload_limit IS NULL OR upload_limit <> 0),
	global_upload_disabled INTEGER NOT NULL CHECK (global_upload_disabled IN (0, 1))
) STRICT;

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

CREATE TABLE operation_torrents (
	operation_id TEXT NOT NULL REFERENCES operations(operation_id) ON DELETE CASCADE,
	torrent_id TEXT NOT NULL CHECK (length(torrent_id) = 36 AND torrent_id = lower(torrent_id)),
	outcome TEXT CHECK (outcome IS NULL OR json_valid(outcome)),
	PRIMARY KEY (operation_id, torrent_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX operation_torrents_by_torrent ON operation_torrents(torrent_id);

INSERT INTO torrents VALUES ('5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f', X'0123456789abcdef0123456789abcdef01234567', NULL,
  'Fixture magnet', X'64343a6e616d65353a612e74787465', 'magnet', NULL,
  'magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567', '/Users/owner/Downloads', 1, 1793795415123456789);
INSERT INTO torrents VALUES ('9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4', X'fedcba9876543210fedcba9876543210fedcba98',
  X'00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff', 'Фікстура – hybrid', X'00ff0064',
  'file', X'64383a616e6e6f756e636531303a687474703a2f2f782f65', NULL, '/Users/owner/Завантаження', 0, 1793795415999999999);
INSERT INTO torrent_files VALUES ('5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f', 0, 'a.txt', 1, 1);
INSERT INTO torrent_files VALUES ('9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4', 0, 'Папка/b.bin', 9223372036854775807, 0);
INSERT INTO torrent_files VALUES ('9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4', 1, 'Папка/c 🎵.flac', 0, 1);
INSERT INTO payload_records VALUES ('5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f', 1, 'apfs', 16777234);
INSERT INTO payload_records VALUES ('9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4', 0, NULL, NULL);
INSERT INTO payload_files VALUES ('5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f', 0, 1, 1, 1793795415123456789, 1793795415123456790, -1, 16777234, 1);
INSERT INTO payload_files VALUES ('9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a5b4', 1, 0, 0, 0, 0, 0, 0, 0);
INSERT INTO settings VALUES (1, '/Users/owner/Downloads', 'dark', 'uk', 1048576, -1, 1);
INSERT INTO operations VALUES ('c0ffee00-1234-4abc-9def-0123456789ab', '0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d', 'pauseTorrents',
  'sha256:0f1e2d', '{"operationId":"c0ffee00-1234-4abc-9def-0123456789ab","torrentIds":["5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f"]}',
  'succeeded', NULL, 1793795415000000000, 1793795415100000000);
INSERT INTO operations VALUES ('d0ffee00-1234-4abc-9def-0123456789ab', '0a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d', 'removeTorrents',
  'sha256:a1b2c3', '{"mode":"recordOnly"}', 'failed', '{"code":"internal.unexpected"}', 1793795416000000000, 1793795416500000000);
INSERT INTO operation_torrents VALUES ('c0ffee00-1234-4abc-9def-0123456789ab', '5d1c9a7e-3b2f-4c6d-8e9f-0a1b2c3d4e5f', '{"ok":true}');
INSERT INTO operation_torrents VALUES ('d0ffee00-1234-4abc-9def-0123456789ab', 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', NULL);
