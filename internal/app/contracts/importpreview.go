package contracts

// ImportPreview is what the user sees before confirming an import (ADD-02, ADD-03, ADD-07,
// ADD-09). Nothing is in the library yet; confirming repeats PreviewVersion so a preview that
// changed in the meantime is not confirmed blindly.
type ImportPreview struct {
	PendingImportID PendingImportID `json:"pendingImportID"`
	PreviewVersion  int             `json:"previewVersion"`
	Source          ImportSource    `json:"source"`
	State           ImportState     `json:"state"`
	// Name, TotalBytes and Files are unknown (null, null, empty) while a magnet waits for metadata.
	Name       *string       `json:"name"`
	Files      []PreviewFile `json:"files"`
	TotalBytes *ByteCount    `json:"totalBytes" ts_type:"string"`
	// Destination is the proposed absolute save folder; null when none is chosen yet.
	Destination *string `json:"destination"`
	// ExistingTorrentID is set when the torrent is already in the library: the UI opens that
	// record and the import changes nothing about it.
	ExistingTorrentID *TorrentID `json:"existingTorrentID"`
}

// PreviewFile is one file of an import preview; Selected is the initial choice.
type PreviewFile struct {
	Index     int       `json:"index"`
	Path      string    `json:"path"`
	SizeBytes ByteCount `json:"sizeBytes" ts_type:"string"`
	Selected  bool      `json:"selected"`
}
