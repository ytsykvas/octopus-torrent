package contracts

import "slices"

// Closed enumerations. Values are part of the wire format: the frontend rejects a value it does not
// know, so adding one is a contract change (ADR api-contract).

// TorrentStatus is the display status of a torrent. This is the vocabulary only; which status wins
// when several conditions hold is decided by the transfer policy (tasks 013, 027).
type TorrentStatus string

const (
	StatusWaitingForMetadata TorrentStatus = "waitingForMetadata"
	StatusChecking           TorrentStatus = "checking"
	StatusDownloading        TorrentStatus = "downloading"
	StatusPaused             TorrentStatus = "paused"
	StatusCompleted          TorrentStatus = "completed"
	StatusSeeding            TorrentStatus = "seeding"
	StatusNoFilesSelected    TorrentStatus = "noFilesSelected"
	StatusError              TorrentStatus = "error"
	StatusDeleting           TorrentStatus = "deleting"
	StatusRelinking          TorrentStatus = "relinking"
)

// ImportSource is how a torrent entered the application (ADD-01, ADD-02, ADD-06, AUTO-01).
type ImportSource string

const (
	SourceFile        ImportSource = "file"
	SourceFinder      ImportSource = "finder"
	SourceDrop        ImportSource = "drop"
	SourceMagnet      ImportSource = "magnet"
	SourceURL         ImportSource = "url"
	SourceWatchFolder ImportSource = "watchFolder"
)

// ImportState is the state of an import preview before confirmation.
type ImportState string

const (
	ImportWaitingForMetadata ImportState = "waitingForMetadata"
	ImportReady              ImportState = "ready"
	ImportFailed             ImportState = "failed"
	ImportCancelled          ImportState = "cancelled"
	ImportExpired            ImportState = "expired"
)

// Theme is the appearance setting (SET-04).
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// Valid reports whether t is one of the themes.
func (t Theme) Valid() bool { return slices.Contains([]Theme{ThemeSystem, ThemeLight, ThemeDark}, t) }

// Language is the interface language (SET-06), an ISO 639-1 code.
type Language string

// Languages are the six supported interface languages.
var Languages = []Language{"en", "uk", "es", "de", "fr", "pt"}

// Valid reports whether l is a supported language.
func (l Language) Valid() bool { return slices.Contains(Languages, l) }

// RemoveMode says what a removal deletes (DEL-01, DEL-02).
type RemoveMode string

const (
	// RemoveRecordOnly removes the library record and keeps the payload files.
	RemoveRecordOnly RemoveMode = "recordOnly"
	// RemoveWithFiles also moves the confirmed payload files to the Trash.
	RemoveWithFiles RemoveMode = "withFiles"
)

// Valid reports whether m is one of the removal modes.
func (m RemoveMode) Valid() bool { return m == RemoveRecordOnly || m == RemoveWithFiles }

// OperationKind names the mutation an operation performs.
type OperationKind string

const (
	OperationPauseTorrents  OperationKind = "pauseTorrents"
	OperationResumeTorrents OperationKind = "resumeTorrents"
	OperationSetFilesWanted OperationKind = "setFilesWanted"
	OperationConfirmImport  OperationKind = "confirmImport"
	OperationRemoveTorrents OperationKind = "removeTorrents"
	OperationUpdateSettings OperationKind = "updateSettings"
)

// OperationState is where an operation is in its lifecycle. States only move forward:
// accepted → running → one of succeeded, failed, cancelled.
type OperationState string

const (
	OperationAccepted  OperationState = "accepted"
	OperationRunning   OperationState = "running"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
	OperationCancelled OperationState = "cancelled"
)

// Finished reports whether the operation has its final state.
func (s OperationState) Finished() bool {
	return s == OperationSucceeded || s == OperationFailed || s == OperationCancelled
}
