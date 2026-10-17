package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestNewUUIDMakesCanonicalVersion4Identifiers(t *testing.T) {
	first, err := NewUUID(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	if err != nil || first != "ffffffff-ffff-4fff-bfff-ffffffffffff" {
		t.Errorf("NewUUID(0xff…) = %q, %v", first, err)
	}
	zero, _ := NewUUID(bytes.NewReader(make([]byte, 16)))
	if zero != "00000000-0000-4000-8000-000000000000" || !IsUUID(zero) {
		t.Errorf("NewUUID(0x00…) = %q", zero)
	}
	if _, err := NewUUID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("NewUUID accepted a short random source")
	}
	if a, b := NewRuntimeID(), NewRuntimeID(); a == b || !IsUUID(string(a)) {
		t.Errorf("NewRuntimeID = %q, %q; want two different UUIDs", a, b)
	}
}

func TestIdentifierFormats(t *testing.T) {
	for value, want := range map[string]bool{
		"3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b": true,
		"3F2A7C1E-5B4D-4E8F-9A6B-1C2D3E4F5A6B": false, // canonical text is lower case
		"3f2a7c1e-5b4d-1e8f-9a6b-1c2d3e4f5a6b": false, // version 1
		"3f2a7c1e-5b4d-4e8f-7a6b-1c2d3e4f5a6b": false, // not the RFC 4122 variant
		"3f2a7c1e5b4d4e8f9a6b1c2d3e4f5a6b":     false,
		"":                                     false,
	} {
		if got := IsUUID(value); got != want {
			t.Errorf("IsUUID(%q) = %v", value, got)
		}
	}
}

// 64-bit quantities cross as decimal strings so JavaScript never rounds them.
func TestSixtyFourBitQuantitiesCrossAsDecimalStrings(t *testing.T) {
	for _, value := range []uint64{0, 9007199254740991, 9007199254740992, 18446744073709551615} {
		encoded, err := json.Marshal(ByteCount(value))
		if err != nil {
			t.Fatal(err)
		}
		var back ByteCount
		if err := json.Unmarshal(encoded, &back); err != nil || uint64(back) != value {
			t.Errorf("%d: encoded %s, decoded %d, %v", value, encoded, back, err)
		}
	}
	for _, v := range []any{BytesPerSecond(1), DurationSeconds(2), Revision(3)} {
		if encoded, _ := json.Marshal(v); string(encoded[0]) != `"` {
			t.Errorf("%T encodes as %s, want a string", v, encoded)
		}
	}
	for _, bad := range []string{`5`, `"-1"`, `"007"`, `""`, `"1e3"`, `"18446744073709551616"`, `" 1"`, `null`} {
		var b ByteCount
		if err := json.Unmarshal([]byte(bad), &b); err == nil && bad != "null" {
			t.Errorf("ByteCount accepted %s", bad)
		}
	}
	var limit *BytesPerSecond
	if err := json.Unmarshal([]byte(`null`), &limit); err != nil || limit != nil {
		t.Errorf("a null limit = %v, %v; want nil (unlimited)", limit, err)
	}
	var d DurationSeconds
	var r Revision
	var s BytesPerSecond
	for _, target := range []json.Unmarshaler{&d, &r, &s} {
		if err := target.UnmarshalJSON([]byte(`"x"`)); err == nil {
			t.Errorf("%T accepted a non-decimal string", target)
		}
		if err := target.UnmarshalJSON([]byte(`"12"`)); err != nil {
			t.Errorf("%T rejected 12: %v", target, err)
		}
	}
}

func TestAppErrorIsAnErrorAndKeepsItsShape(t *testing.T) {
	op, torrent := OperationID("c0ffee00-1234-4abc-9def-0123456789ab"), TorrentID("3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b")
	e := NewAppError(CodeOperationNotFound, map[string]string{"runtimeID": "r"}).WithOperation(op).WithTorrent(torrent)
	var asError error = e
	if asError.Error() != CodeOperationNotFound || *e.OperationID != op || *e.TorrentID != torrent {
		t.Errorf("AppError = %+v, Error() = %q", e, asError.Error())
	}
	encoded, _ := json.Marshal(NewAppError(CodeInternalUnexpected, nil))
	if string(encoded) != `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}` {
		t.Errorf("AppError JSON = %s", encoded)
	}
	var target *AppError
	if !errors.As(error(InvalidRequest("wanted", ReasonMissing)), &target) || target.Params["field"] != "wanted" {
		t.Errorf("InvalidRequest = %+v", target)
	}
}

// The fingerprint identifies a request for idempotency: it ignores the operation ID and the order
// and repetition of ID sets, and changes with anything that changes the mutation.
func TestFingerprintNormalizesTheRequest(t *testing.T) {
	const a, b = "3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b", "7d4e5f6a-1b2c-4d3e-8f4a-5b6c7d8e9f0a"
	same := []Request{
		&SetFilesWantedRequest{OperationID: "c0ffee00-1234-4abc-9def-0123456789ab", TorrentID: a, FileIndexes: []int{1, 0, 1}, Wanted: true},
		&SetFilesWantedRequest{OperationID: "d0ffee00-1234-4abc-9def-0123456789ab", TorrentID: a, FileIndexes: []int{0, 1}, Wanted: true},
	}
	if Fingerprint(same[0]) != Fingerprint(same[1]) {
		t.Error("the operation ID or the order of file indexes changed the fingerprint")
	}
	different := &SetFilesWantedRequest{TorrentID: a, FileIndexes: []int{0, 1}, Wanted: false}
	if Fingerprint(same[0]) == Fingerprint(different) {
		t.Error("wanted=false has the fingerprint of wanted=true")
	}
	pause1 := &TorrentSelectionRequest{TorrentIDs: []TorrentID{b, a, a}}
	pause2 := &TorrentSelectionRequest{TorrentIDs: []TorrentID{a, b}}
	remove1 := &RemoveTorrentsRequest{TorrentIDs: []TorrentID{b, a}, Mode: RemoveRecordOnly}
	remove2 := &RemoveTorrentsRequest{TorrentIDs: []TorrentID{a, b}, Mode: RemoveRecordOnly}
	confirm1 := &ConfirmImportRequest{SelectedFileIndexes: []int{2, 0}}
	confirm2 := &ConfirmImportRequest{SelectedFileIndexes: []int{0, 2, 2}}
	settings := &UpdateSettingsRequest{OperationID: "c0ffee00-1234-4abc-9def-0123456789ab"}
	cancel := &CancelImportRequest{PendingImportID: "5e6f7a8b-9c0d-4e1f-a2b3-c4d5e6f7a8b9"}
	if Fingerprint(pause1) != Fingerprint(pause2) || Fingerprint(remove1) != Fingerprint(remove2) || Fingerprint(confirm1) != Fingerprint(confirm2) {
		t.Error("ID sets must be compared as sets")
	}
	if Fingerprint(settings) == "" || Fingerprint(cancel) == "" || len(Fingerprint(settings)) != 64 {
		t.Errorf("fingerprints must be SHA-256 hex: %q", Fingerprint(settings))
	}
	if !reflect.DeepEqual(same[0].(*SetFilesWantedRequest).FileIndexes, []int{1, 0, 1}) {
		t.Error("Fingerprint changed the request it was given")
	}
	// every field that changes the mutation changes the fingerprint
	plan := "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	for name, pair := range map[string][2]Request{
		"TorrentSelection.TorrentIDs":       {&TorrentSelectionRequest{TorrentIDs: []TorrentID{a}}, &TorrentSelectionRequest{TorrentIDs: []TorrentID{a, b}}},
		"SetFilesWanted.TorrentID":          {&SetFilesWantedRequest{TorrentID: a, FileIndexes: []int{0}}, &SetFilesWantedRequest{TorrentID: b, FileIndexes: []int{0}}},
		"SetFilesWanted.FileIndexes":        {&SetFilesWantedRequest{TorrentID: a, FileIndexes: []int{0}}, &SetFilesWantedRequest{TorrentID: a, FileIndexes: []int{0, 1}}},
		"RemoveTorrents.TorrentIDs":         {&RemoveTorrentsRequest{TorrentIDs: []TorrentID{a}, Mode: RemoveRecordOnly}, &RemoveTorrentsRequest{TorrentIDs: []TorrentID{b}, Mode: RemoveRecordOnly}},
		"RemoveTorrents.Mode":               {&RemoveTorrentsRequest{TorrentIDs: []TorrentID{a}, Mode: RemoveRecordOnly}, &RemoveTorrentsRequest{TorrentIDs: []TorrentID{a}, Mode: RemoveWithFiles}},
		"RemoveTorrents.DeletionPlanID":     {&RemoveTorrentsRequest{TorrentIDs: []TorrentID{a}, Mode: RemoveWithFiles}, &RemoveTorrentsRequest{TorrentIDs: []TorrentID{a}, Mode: RemoveWithFiles, DeletionPlanID: &plan}},
		"ConfirmImport.PendingImportID":     {&ConfirmImportRequest{PendingImportID: a}, &ConfirmImportRequest{PendingImportID: b}},
		"ConfirmImport.PreviewVersion":      {&ConfirmImportRequest{PreviewVersion: 1}, &ConfirmImportRequest{PreviewVersion: 2}},
		"ConfirmImport.SelectedFileIndexes": {&ConfirmImportRequest{SelectedFileIndexes: []int{0}}, &ConfirmImportRequest{SelectedFileIndexes: []int{1}}},
		"ConfirmImport.Destination":         {&ConfirmImportRequest{Destination: "/a"}, &ConfirmImportRequest{Destination: "/b"}},
		"ConfirmImport.StartAfterAdding":    {&ConfirmImportRequest{}, &ConfirmImportRequest{StartAfterAdding: true}},
		"CancelImport.PendingImportID":      {&CancelImportRequest{PendingImportID: a}, &CancelImportRequest{PendingImportID: b}},
		"UpdateSettings.Settings":           {&UpdateSettingsRequest{}, &UpdateSettingsRequest{Settings: Settings{Language: "uk"}}},
	} {
		if Fingerprint(pair[0]) == Fingerprint(pair[1]) {
			t.Errorf("%s: a different value has the same fingerprint", name)
		}
	}
}

// A request is decoded field by field: a value of the wrong type is a request problem named by its
// field, not a failure of the whole call; only a payload that is not an object fails to decode.
func TestRequestsDecodeFieldByField(t *testing.T) {
	var r SetFilesWantedRequest
	if err := json.Unmarshal([]byte(`[1]`), &r); err == nil {
		t.Error("a non-object request decoded")
	}
	if err := json.Unmarshal([]byte(`{"operationID":"c0ffee00-1234-4abc-9def-0123456789ab","torrentID":"3f2a7c1e-5b4d-4e8f-9a6b-1c2d3e4f5a6b","fileIndexes":[0],"wanted":false}`), &r); err != nil || r.Validate() != nil || r.Wanted {
		t.Errorf("wanted=false: %+v, %v, %+v", r, err, r.Validate())
	}
	var empty CancelImportRequest
	if p := empty.Validate(); p == nil || p.Params["field"] != "pendingImportID" || p.Params["reason"] != ReasonMissing {
		t.Errorf("a request built in Go without its fields: %+v", p)
	}
}

// The generated TypeScript models must not claim that a 64-bit quantity is a number.
func TestEverySixtyFourBitFieldIsGeneratedAsAString(t *testing.T) {
	decimal := map[reflect.Type]bool{
		reflect.TypeFor[ByteCount](): true, reflect.TypeFor[BytesPerSecond](): true,
		reflect.TypeFor[DurationSeconds](): true, reflect.TypeFor[Revision](): true,
	}
	checked := 0
	var walk func(reflect.Type, map[reflect.Type]bool)
	walk = func(typ reflect.Type, seen map[reflect.Type]bool) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		for i := range typ.NumField() {
			field := typ.Field(i)
			ft := field.Type
			for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
				ft = ft.Elem()
			}
			if decimal[ft] {
				checked++
				if field.Tag.Get("ts_type") != "string" {
					t.Errorf("%s.%s is a 64-bit quantity without ts_type:\"string\"", typ.Name(), field.Name)
				}
			}
			if ft.Kind() == reflect.Struct && field.IsExported() {
				walk(ft, seen)
			}
		}
	}
	seen := map[reflect.Type]bool{}
	for _, make := range outgoing {
		walk(reflect.TypeOf(make()).Elem(), seen)
	}
	for _, make := range incoming {
		walk(reflect.TypeOf(make()).Elem(), seen)
	}
	if checked == 0 {
		t.Fatal("no 64-bit field found")
	}
}

// Only succeeded, failed and cancelled are final; an operation never leaves a final state.
func TestOnlyTheLastOperationStatesAreFinished(t *testing.T) {
	finished := map[OperationState]bool{
		OperationAccepted: false, OperationRunning: false,
		OperationSucceeded: true, OperationFailed: true, OperationCancelled: true,
	}
	for state, want := range finished {
		if state.Finished() != want {
			t.Errorf("%s.Finished() = %v", state, !want)
		}
	}
}
