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
)

// ErrorCodes lists every code Go sends (testdata/vocabulary.json); the frontend adds its own.
var ErrorCodes = []string{
	CodeRequestInvalid, CodeOperationIDConflict, CodeOperationNotFound, CodeOperationCancelled,
	CodeInternalUnexpected,
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
// localization and are always present (an empty object when there are none).
type AppError struct {
	Code        string            `json:"code"`
	OperationID *OperationID      `json:"operationID"`
	TorrentID   *TorrentID        `json:"torrentID"`
	Params      map[string]string `json:"params"`
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
