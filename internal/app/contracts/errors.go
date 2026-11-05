package contracts

// Stable error codes, "<area>.<problem>". A code is never UI text: the frontend maps it to a
// localized message with Params, and shows a fallback for a code it does not know. Each task that
// adds a code adds it here, to the ADR table and to the TypeScript catalogue.
const (
	// CodeRequestInvalid: a request failed validation; Params "field" (JSON path) and "reason".
	CodeRequestInvalid = "request.invalid"
	// CodeOperationIDConflict: the operation ID was already used for a different request.
	CodeOperationIDConflict = "operation.idConflict"
	// CodeOperationNotFound: this runtime has no operation with that ID; Params "runtimeID". When
	// that runtime differs from the one that accepted the operation, its outcome is unknown: read
	// the full state, never resend the command because of this answer.
	CodeOperationNotFound = "operation.notFound"
	// CodeOperationCancelled: the operation was cancelled before it took effect.
	CodeOperationCancelled = "operation.cancelled"
	// CodeInternalUnexpected: an error without a code of its own; details stay in the local log.
	CodeInternalUnexpected = "internal.unexpected"
	// CodeTorrentNotFound: the library has no torrent with the error's TorrentID.
	CodeTorrentNotFound = "torrent.notFound"
	// CodeTorrentDuplicate: the library already has this torrent; TorrentID is the existing record.
	CodeTorrentDuplicate = "torrent.duplicate"
)

// Codes of the library (task 015): why a change was not stored, or why the library cannot be used.
// Each is the answer of internal/library's one mapping (library.Problem); its cause stays in
// diagnostics.
const (
	// CodeLibraryBusy: another process holds the library's write lock.
	CodeLibraryBusy = "library.busy"
	// CodeLibraryDiskFull: the disk holding the library is full; nothing was stored.
	CodeLibraryDiskFull = "library.diskFull"
	// CodeLibraryReadOnly: the library's file or folder cannot be written.
	CodeLibraryReadOnly = "library.readOnly"
	// CodeLibraryUnavailable: the library's file cannot be opened or read, or was deleted, moved or
	// replaced while the application ran.
	CodeLibraryUnavailable = "library.unavailable"
	// CodeLibraryDamaged: the library's file is damaged or not a database.
	CodeLibraryDamaged = "library.damaged"
	// CodeLibraryOutcomeUnknown: the save failed while it was being committed; whether it was
	// stored is known only after the next start.
	CodeLibraryOutcomeUnknown = "library.outcomeUnknown"
	// CodeLibraryNewerVersion: a newer version of the application wrote the library.
	CodeLibraryNewerVersion = "library.newerVersion"
	// CodeLibraryNotALibrary: the file in the library's place is not this application's library.
	CodeLibraryNotALibrary = "library.notALibrary"
	// CodeLibraryMigrationFailed: the library could not be brought to the current version; it is
	// unchanged.
	CodeLibraryMigrationFailed = "library.migrationFailed"
)

// ErrorCodes lists every code Go sends (testdata/vocabulary.json); the frontend adds its own.
var ErrorCodes = []string{
	CodeRequestInvalid, CodeOperationIDConflict, CodeOperationNotFound, CodeOperationCancelled,
	CodeInternalUnexpected, CodeTorrentNotFound, CodeTorrentDuplicate, CodeLibraryBusy, CodeLibraryDiskFull,
	CodeLibraryReadOnly, CodeLibraryUnavailable, CodeLibraryDamaged, CodeLibraryOutcomeUnknown,
	CodeLibraryNewerVersion, CodeLibraryNotALibrary, CodeLibraryMigrationFailed,
}

// Reasons of CodeRequestInvalid.
const (
	ReasonMissing = "missing" // the key is absent
	ReasonNull    = "null"    // null where a value is required
	ReasonUnknown = "unknown" // a key the request does not have
	ReasonType    = "type"    // a value of the wrong JSON type or out of the type's range
	ReasonFormat  = "format"  // a string in the wrong format (UUID, absolute path)
	ReasonRange   = "range"   // a number outside its allowed range
	ReasonEmpty   = "empty"   // an empty list where at least one item is required
	ReasonValue   = "value"   // a value outside its enumeration
)

// AppError is the error every facade method returns and every DTO carries. Code is stable;
// OperationID and TorrentID are null when the error is not about one; Params are strings for
// localization and are always present (an empty object when there are none). The cause, when there
// is one, is for diagnostics only: it is never encoded.
type AppError struct {
	Code        string            `json:"code"`
	OperationID *OperationID      `json:"operationID"`
	TorrentID   *TorrentID        `json:"torrentID"`
	Params      map[string]string `json:"params"`

	cause error
}

// NewAppError makes an error with the code and the localization parameters.
func NewAppError(code string, params map[string]string) *AppError {
	if params == nil {
		params = map[string]string{}
	}
	return &AppError{Code: code, Params: params}
}

// Error returns the code: the error is for programs and logs, not for display.
func (e *AppError) Error() string { return e.Code }

// WithCause records what caused the error, for diagnostics and errors.Is; it never crosses the
// bridge.
func (e *AppError) WithCause(cause error) *AppError {
	e.cause = cause
	return e
}

// Unwrap returns the cause, or nil.
func (e *AppError) Unwrap() error { return e.cause }

// WithOperation records the operation the error belongs to.
func (e *AppError) WithOperation(id OperationID) *AppError {
	e.OperationID = &id
	return e
}

// WithTorrent records the torrent the error belongs to.
func (e *AppError) WithTorrent(id TorrentID) *AppError {
	e.TorrentID = &id
	return e
}

// InvalidRequest is CodeRequestInvalid for one field (a JSON path such as "fileIndexes[1]").
func InvalidRequest(field, reason string) *AppError {
	return NewAppError(CodeRequestInvalid, map[string]string{"field": field, "reason": reason})
}
