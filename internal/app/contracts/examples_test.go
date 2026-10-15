package contracts

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// manifestEntry describes one shared example. The TypeScript tests read the same manifest: Go checks
// what Go produces (toFrontend, valid) and what Go accepts (toGo); TypeScript checks what it accepts
// from Go (toFrontend) and what it sends (toGo, valid).
type manifestEntry struct {
	File      string `json:"file"`
	Type      string `json:"type"`
	Direction string `json:"direction"`
	Valid     bool   `json:"valid"`
	Path      string `json:"path"`
	Field     string `json:"field"`
	Reason    string `json:"reason"`
}

func readManifest(t *testing.T) []manifestEntry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []manifestEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func readExample(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// outgoing are the DTOs Go sends to the frontend.
var outgoing = map[string]func() any{
	"LibrarySnapshot": func() any { return new(LibrarySnapshot) },
	"LibraryDelta":    func() any { return new(LibraryDelta) },
	"TorrentSnapshot": func() any { return new(TorrentSnapshot) },
	"ImportPreview":   func() any { return new(ImportPreview) },
	"Settings":        func() any { return new(Settings) },
	"OperationStatus": func() any { return new(OperationStatus) },
	"AppError":        func() any { return new(AppError) },
}

// incoming are the requests the frontend sends to Go.
var incoming = map[string]func() Request{
	"SetFilesWantedRequest":   func() Request { return new(SetFilesWantedRequest) },
	"TorrentSelectionRequest": func() Request { return new(TorrentSelectionRequest) },
	"ConfirmImportRequest":    func() Request { return new(ConfirmImportRequest) },
	"CancelImportRequest":     func() Request { return new(CancelImportRequest) },
	"RemoveTorrentsRequest":   func() Request { return new(RemoveTorrentsRequest) },
	"UpdateSettingsRequest":   func() Request { return new(UpdateSettingsRequest) },
}

// Every valid outgoing example is exactly what Go encodes: decoding it into the Go type (no unknown
// keys) and encoding it again gives the same JSON value, with every key present, nulls included.
func TestGoEncodesEveryOutgoingExampleExactly(t *testing.T) {
	for _, e := range readManifest(t) {
		if e.Direction != "toFrontend" || !e.Valid {
			continue
		}
		raw := readExample(t, e.File)
		value := outgoing[e.Type]()
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(value); err != nil {
			t.Errorf("%s: decode %s: %v", e.File, e.Type, err)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Errorf("%s: encode: %v", e.File, err)
			continue
		}
		var want, got any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encoded, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: Go encodes\n%s\nwant the example", e.File, encoded)
		}
	}
}

// Every incoming example is accepted or rejected by the request's own validation, naming the field.
func TestGoValidatesEveryIncomingExample(t *testing.T) {
	seen := 0
	for _, e := range readManifest(t) {
		if e.Direction != "toGo" {
			continue
		}
		seen++
		request := incoming[e.Type]()
		if err := json.Unmarshal(readExample(t, e.File), request); err != nil {
			t.Errorf("%s: a request never fails to decode on its shape, got %v", e.File, err)
			continue
		}
		problem := request.Validate()
		switch {
		case e.Valid && problem != nil:
			t.Errorf("%s: rejected: %+v", e.File, problem)
		case !e.Valid && problem == nil:
			t.Errorf("%s: accepted, want %s %s", e.File, e.Field, e.Reason)
		case !e.Valid && (problem.Code != CodeRequestInvalid || problem.Params["field"] != e.Field || problem.Params["reason"] != e.Reason):
			t.Errorf("%s: got %+v, want %s field %s reason %s", e.File, problem, CodeRequestInvalid, e.Field, e.Reason)
		}
	}
	if seen == 0 {
		t.Fatal("the manifest has no incoming examples")
	}
}

func TestTheManifestNamesOnlyKnownTypesAndExistingFiles(t *testing.T) {
	for _, e := range readManifest(t) {
		if _, err := os.Stat(filepath.Join("testdata", e.File)); err != nil {
			t.Errorf("%s: %v", e.File, err)
		}
		_, out := outgoing[e.Type]
		_, in := incoming[e.Type]
		if (e.Direction == "toFrontend" && !out) || (e.Direction == "toGo" && !in) {
			t.Errorf("%s: unknown %s type %s", e.File, e.Direction, e.Type)
		}
	}
}
