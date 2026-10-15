package contracts

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Request is a command DTO the frontend sends. The facade calls Validate before anything else, even
// though the UI validated it too (STACK-01).
type Request interface {
	// Validate returns request.invalid naming the first bad field, or nil.
	Validate() *AppError
	// normalized returns a copy for comparison: no operation ID, no shape, ID sets sorted and
	// without repeats.
	normalized() Request
}

// Fingerprint identifies what a request asks for, so a repeated operation ID can be recognized as
// the same request (run once) or a different one (rejected). It is the SHA-256 of the normalized
// request in hex.
func Fingerprint(r Request) string {
	data, _ := json.Marshal(r.normalized()) // plain structs of strings, numbers and slices
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TorrentSelectionRequest pauses or resumes torrents (TR-01, UI-07); the facade method decides
// which. A non-trivial mutation: it carries an operation ID.
type TorrentSelectionRequest struct {
	OperationID OperationID `json:"operationID"`
	TorrentIDs  []TorrentID `json:"torrentIDs"`

	shape shape
}

// SetFilesWantedRequest selects or skips files of one torrent (TR-06, TR-07). A folder is sent as
// the indexes of its files.
type SetFilesWantedRequest struct {
	OperationID OperationID `json:"operationID"`
	TorrentID   TorrentID   `json:"torrentID"`
	FileIndexes []int       `json:"fileIndexes"`
	Wanted      bool        `json:"wanted"`

	shape shape
}

// ConfirmImportRequest confirms an import preview (ADD-07): at least one file, an absolute
// destination, and the preview version the user saw.
type ConfirmImportRequest struct {
	OperationID         OperationID     `json:"operationID"`
	PendingImportID     PendingImportID `json:"pendingImportID"`
	PreviewVersion      int             `json:"previewVersion"`
	SelectedFileIndexes []int           `json:"selectedFileIndexes"`
	Destination         string          `json:"destination"`
	StartAfterAdding    bool            `json:"startAfterAdding"`

	shape shape
}

// CancelImportRequest discards an import preview; it changes nothing in the library, so it needs
// no operation ID.
type CancelImportRequest struct {
	PendingImportID PendingImportID `json:"pendingImportID"`

	shape shape
}

// RemoveTorrentsRequest removes records (DEL-01, DEL-02). With files, DeletionPlanID names the
// deletion plan Go issued when it built the file list the user confirmed: a UUID bound to exactly
// these torrents and files, usable once, valid only in the runtime that issued it (031/032). A
// number the frontend could make up would prove nothing. For a record-only removal it is null.
type RemoveTorrentsRequest struct {
	OperationID    OperationID `json:"operationID"`
	TorrentIDs     []TorrentID `json:"torrentIDs"`
	Mode           RemoveMode  `json:"mode"`
	DeletionPlanID *string     `json:"deletionPlanID"`

	shape shape
}

// UpdateSettingsRequest saves all settings at once.
type UpdateSettingsRequest struct {
	OperationID OperationID `json:"operationID"`
	Settings    Settings    `json:"settings"`

	shape shape
}

func (r *TorrentSelectionRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *SetFilesWantedRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *ConfirmImportRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *CancelImportRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *RemoveTorrentsRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *UpdateSettingsRequest) UnmarshalJSON(data []byte) (err error) {
	r.shape, err = decodeFields(data, r)
	return err
}

func (r *TorrentSelectionRequest) Validate() *AppError {
	return firstProblem(
		r.shape.problem(""),
		CheckUUID("operationID", string(r.OperationID)),
		checkTorrentIDs(r.TorrentIDs),
	)
}

func (r *SetFilesWantedRequest) Validate() *AppError {
	return firstProblem(
		r.shape.problem(""),
		CheckUUID("operationID", string(r.OperationID)),
		CheckUUID("torrentID", string(r.TorrentID)),
		checkFileIndexes("fileIndexes", r.FileIndexes),
	)
}

func (r *ConfirmImportRequest) Validate() *AppError {
	return firstProblem(
		r.shape.problem(""),
		CheckUUID("operationID", string(r.OperationID)),
		CheckUUID("pendingImportID", string(r.PendingImportID)),
		checkPositive("previewVersion", r.PreviewVersion),
		checkFileIndexes("selectedFileIndexes", r.SelectedFileIndexes),
		checkFolderPath("destination", r.Destination),
	)
}

func (r *CancelImportRequest) Validate() *AppError {
	return firstProblem(r.shape.problem(""), CheckUUID("pendingImportID", string(r.PendingImportID)))
}

func (r *RemoveTorrentsRequest) Validate() *AppError {
	var plan *AppError
	switch {
	case !r.Mode.Valid():
		plan = InvalidRequest("mode", ReasonValue)
	case r.Mode == RemoveWithFiles && r.DeletionPlanID == nil:
		plan = InvalidRequest("deletionPlanID", ReasonNull)
	case r.Mode == RemoveWithFiles:
		plan = CheckUUID("deletionPlanID", *r.DeletionPlanID)
	case r.DeletionPlanID != nil:
		plan = InvalidRequest("deletionPlanID", ReasonValue)
	}
	return firstProblem(
		r.shape.problem(""),
		CheckUUID("operationID", string(r.OperationID)),
		checkTorrentIDs(r.TorrentIDs),
		plan,
	)
}

func (r *UpdateSettingsRequest) Validate() *AppError {
	return firstProblem(
		r.shape.problem(""),
		CheckUUID("operationID", string(r.OperationID)),
		r.Settings.validate("settings."),
	)
}

func (r *TorrentSelectionRequest) normalized() Request {
	return &TorrentSelectionRequest{TorrentIDs: sortedUnique(r.TorrentIDs)}
}

func (r *SetFilesWantedRequest) normalized() Request {
	return &SetFilesWantedRequest{TorrentID: r.TorrentID, FileIndexes: sortedUnique(r.FileIndexes), Wanted: r.Wanted}
}

func (r *ConfirmImportRequest) normalized() Request {
	n := *r
	n.OperationID, n.shape, n.SelectedFileIndexes = "", nil, sortedUnique(r.SelectedFileIndexes)
	return &n
}

func (r *CancelImportRequest) normalized() Request {
	return &CancelImportRequest{PendingImportID: r.PendingImportID}
}

func (r *RemoveTorrentsRequest) normalized() Request {
	n := *r
	n.OperationID, n.shape, n.TorrentIDs = "", nil, sortedUnique(r.TorrentIDs)
	return &n
}

func (r *UpdateSettingsRequest) normalized() Request {
	return &UpdateSettingsRequest{Settings: r.Settings}
}

// firstProblem returns the first non-nil problem: the order of the checks is the order of fields.
func firstProblem(problems ...*AppError) *AppError {
	for _, p := range problems {
		if p != nil {
			return p
		}
	}
	return nil
}

// CheckUUID returns request.invalid for field when value is empty (missing) or not a canonical
// version 4 UUID (format), or nil.
func CheckUUID(field, value string) *AppError {
	switch {
	case value == "":
		return InvalidRequest(field, ReasonMissing)
	case !IsUUID(value):
		return InvalidRequest(field, ReasonFormat)
	}
	return nil
}

func checkTorrentIDs(ids []TorrentID) *AppError {
	if len(ids) == 0 {
		return InvalidRequest("torrentIDs", ReasonEmpty)
	}
	for i, id := range ids {
		if p := CheckUUID(fmt.Sprintf("torrentIDs[%d]", i), string(id)); p != nil {
			return p
		}
	}
	return nil
}

// checkFileIndexes requires at least one index, none negative. The upper bound is the torrent's
// file count, which only the service knows.
func checkFileIndexes(field string, indexes []int) *AppError {
	if len(indexes) == 0 {
		return InvalidRequest(field, ReasonEmpty)
	}
	for i, index := range indexes {
		if index < 0 {
			return InvalidRequest(fmt.Sprintf("%s[%d]", field, i), ReasonRange)
		}
	}
	return nil
}

func checkPositive(field string, value int) *AppError {
	if value < 1 {
		return InvalidRequest(field, ReasonRange)
	}
	return nil
}

// maxPathBytes bounds a folder path from the frontend; macOS paths are at most 1024 bytes.
const maxPathBytes = 1024

// checkFolderPath accepts only a clean absolute path below the root, without control characters
// (a NUL would truncate it in C APIs) and at most maxPathBytes long. This is the lexical check of
// the contract; the file service still resolves symlinks and checks the folder before any write.
func checkFolderPath(field, path string) *AppError {
	if len(path) > maxPathBytes || path == "/" || !filepath.IsAbs(path) || filepath.Clean(path) != path ||
		strings.ContainsFunc(path, unicode.IsControl) {
		return InvalidRequest(field, ReasonFormat)
	}
	return nil
}

func sortedUnique[T cmp.Ordered](values []T) []T {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return slices.Compact(sorted)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
