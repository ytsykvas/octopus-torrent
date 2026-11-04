-- Piece-completion store schema 1 (task 009's table, under task 014's migration policy). A released
-- migration never changes: a change is the next numbered file. One advisory mark per piece of a
-- torrent: complete means its hash matched. STRICT, like every library table.
CREATE TABLE piece_completion (
	infohash BLOB NOT NULL,
	piece INTEGER NOT NULL,
	complete INTEGER NOT NULL,
	PRIMARY KEY (infohash, piece)
) STRICT, WITHOUT ROWID;
