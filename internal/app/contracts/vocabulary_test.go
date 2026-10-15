package contracts

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// testdata/vocabulary.json is the one list of every closed enumeration and error code; the
// TypeScript tests compare their lists with the same file, so neither side can add a value alone.
func TestEnumerationsAndErrorCodesMatchTheSharedVocabulary(t *testing.T) {
	var vocabulary map[string][]string
	if err := json.Unmarshal(readExample(t, "vocabulary.json"), &vocabulary); err != nil {
		t.Fatal(err)
	}
	for name, values := range map[string][]string{
		"torrentStatuses": texts(TorrentStatuses),
		"importSources":   texts(ImportSources),
		"importStates":    texts(ImportStates),
		"themes":          texts(Themes),
		"languages":       texts(Languages),
		"removeModes":     texts(RemoveModes),
		"operationKinds":  texts(OperationKinds),
		"operationStates": texts(OperationStates),
		"goErrorCodes":    ErrorCodes,
	} {
		if !reflect.DeepEqual(values, vocabulary[name]) {
			t.Errorf("%s = %v, vocabulary.json says %v", name, values, vocabulary[name])
		}
	}
}

func texts[T ~string](values []T) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v)
	}
	return out
}

// Go never sends null for a list or a time outside UTC, whatever value a service builds: the
// frontend rejects both as contract violations.
func TestZeroValuesEncodeAsEmptyListsAndUTCTimes(t *testing.T) {
	berlin := time.FixedZone("CEST", 2*60*60)
	for name, tc := range map[string]struct {
		value any
		want  []string
	}{
		"LibrarySnapshot": {LibrarySnapshot{}, []string{`"torrents":[]`}},
		"LibraryDelta":    {LibraryDelta{}, []string{`"upserted":[]`, `"removed":[]`}},
		"TorrentSnapshot": {TorrentSnapshot{AddedAt: time.Date(2026, 10, 15, 14, 0, 0, 0, berlin)}, []string{`"files":[]`, `"errors":[]`, `"addedAt":"2026-10-15T12:00:00Z"`}},
		"ImportPreview":   {ImportPreview{}, []string{`"files":[]`}},
		"AppError":        {AppError{Code: CodeInternalUnexpected}, []string{`"params":{}`}},
		// A corrupt stored time must not make the whole snapshot unencodable (Wails would exit).
		"TorrentSnapshot from year 10000": {TorrentSnapshot{AddedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, []string{`"addedAt":"0001-01-01T00:00:00Z"`}},
	} {
		encoded, err := json.Marshal(tc.value)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, part := range tc.want {
			if !bytes.Contains(encoded, []byte(part)) {
				t.Errorf("%s encodes as %s, want %s in it", name, encoded, part)
			}
		}
	}
}
