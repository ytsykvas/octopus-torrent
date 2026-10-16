package contracts

import "time"

// LibrarySnapshot is the full state of the library at one revision of one runtime. It is
// authoritative: the frontend replaces its state with it. Revisions may be skipped between full
// snapshots because updates are merged; that is never a reason to fetch again. Torrents is never
// null.
type LibrarySnapshot struct {
	RuntimeID RuntimeID         `json:"runtimeID"`
	Revision  Revision          `json:"revision" ts_type:"string"`
	Torrents  []TorrentSnapshot `json:"torrents"`
}

// LibraryDelta is a partial update. It applies only on top of BaseRevision of the same runtime and
// moves the library to Revision; a frontend at another revision fetches a full snapshot instead.
type LibraryDelta struct {
	RuntimeID    RuntimeID         `json:"runtimeID"`
	BaseRevision Revision          `json:"baseRevision" ts_type:"string"`
	Revision     Revision          `json:"revision" ts_type:"string"`
	Upserted     []TorrentSnapshot `json:"upserted"` // full new state of each changed torrent
	Removed      []TorrentID       `json:"removed"`
}

// TorrentSnapshot is everything the card and the inspector show about one torrent, taken at one
// revision so both show the same values (UI-01, UI-02, TR-02).
type TorrentSnapshot struct {
	ID TorrentID `json:"id"`
	// InfohashV1 and InfohashV2 are the full infohashes in lower-case hex; null when the torrent has
	// no such protocol version. At least one is present.
	InfohashV1 *string `json:"infohashV1"`
	InfohashV2 *string `json:"infohashV2"`
	// Name comes from the torrent: untrusted text, displayed as is, never translated.
	Name   string        `json:"name"`
	Status TorrentStatus `json:"status"`
	// ManualPaused is the stored user pause (no payload in either direction). UploadAllowed is the
	// effective upload permission Go computes from the pause, the global upload switch and errors.
	ManualPaused  bool `json:"manualPaused"`
	UploadAllowed bool `json:"uploadAllowed"`
	// TotalBytes and WantedBytes (selected files only, pad files excluded) are null until the
	// metadata is known; "0" means known and empty.
	TotalBytes  *ByteCount `json:"totalBytes" ts_type:"string"`
	WantedBytes *ByteCount `json:"wantedBytes" ts_type:"string"`
	// CompletedBytes is verified data of wanted files; it can decrease after a recheck.
	CompletedBytes ByteCount `json:"completedBytes" ts_type:"string"`
	// Transfer rates are "0" when nothing is transferred.
	DownloadBytesPerSecond BytesPerSecond `json:"downloadBytesPerSecond" ts_type:"string"`
	UploadBytesPerSecond   BytesPerSecond `json:"uploadBytesPerSecond" ts_type:"string"`
	PeerCount              int            `json:"peerCount"`
	// ETASeconds is null when it cannot be computed (not downloading, or no download rate); "0"
	// means less than a second.
	ETASeconds *DurationSeconds `json:"etaSeconds" ts_type:"string"`
	// SavePath is the absolute payload folder, full Unicode, as shown and copied (UI-12).
	SavePath string       `json:"savePath"`
	Source   ImportSource `json:"source"`
	// AddedAt is in UTC, RFC 3339 on the wire.
	AddedAt time.Time `json:"addedAt" ts_type:"string"`
	// FileCount and SelectedFileCount are null until the metadata is known.
	FileCount         *int          `json:"fileCount"`
	SelectedFileCount *int          `json:"selectedFileCount"`
	Files             []TorrentFile `json:"files"`  // empty until the metadata is known
	Errors            []AppError    `json:"errors"` // the torrent's current problems
}

// TorrentFile is one file of a torrent.
type TorrentFile struct {
	// Index is the engine's file index, 0…FileCount−1; pad files keep their index and are hidden by
	// the file tree.
	Index int `json:"index"`
	// Path is relative, "/"-separated and without the torrent's root folder; untrusted text.
	// Folders are derived from paths: there are no folder identifiers on the wire.
	Path           string    `json:"path"`
	SizeBytes      ByteCount `json:"sizeBytes" ts_type:"string"`
	CompletedBytes ByteCount `json:"completedBytes" ts_type:"string"`
	Wanted         bool      `json:"wanted"`
}
