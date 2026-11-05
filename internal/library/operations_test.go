package library

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
)

// operation reads one journal entry, failing the test on an error or when it is missing.
func operation(t *testing.T, lib *Library, id contracts.OperationID) Operation {
	t.Helper()
	op, found, err := lib.Operation(deadline(t, 5*time.Second), id)
	if err != nil || !found {
		t.Fatalf("Operation(%s) = found %v, %v", id, found, err)
	}
	return op
}

// sameOperation compares journal entries; times must be the same instant, and in UTC when read back
// (a zero FinishedAt stays zero).
func sameOperation(t *testing.T, got, want Operation) {
	t.Helper()
	for name, pair := range map[string][2]time.Time{"accepted": {got.AcceptedAt, want.AcceptedAt}, "finished": {got.FinishedAt, want.FinishedAt}} {
		if !pair[0].Equal(pair[1]) || !pair[0].IsZero() && pair[0].Location() != time.UTC {
			t.Errorf("%s at %v, want %v in UTC", name, pair[0], pair[1])
		}
	}
	got.AcceptedAt, got.FinishedAt, want.AcceptedAt, want.FinishedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Operation =\n%+v\nwant\n%+v", got, want)
	}
}

// intentRead is what the journal holds for an intent added with one torrent, before it finishes.
func intentRead(t *testing.T, op OperationIntent, torrent contracts.TorrentID) Operation {
	t.Helper()
	request, err := json.Marshal(op.Request)
	if err != nil {
		t.Fatal(err)
	}
	return Operation{
		ID: op.ID, RuntimeID: op.RuntimeID, Kind: op.Kind, Fingerprint: contracts.Fingerprint(op.Request),
		Request: request, State: contracts.OperationRunning, AcceptedAt: op.AcceptedAt,
		Torrents: []OperationTorrent{{ID: torrent}},
	}
}

func TestAnImportIsRecordedAsARunningOperation(t *testing.T) {
	lib, path := openLibrary(t)
	op := importIntent(opA, newID(t))
	if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idA, 1), op); err != nil {
		t.Fatal(err)
	}
	sameOperation(t, operation(t, reopen(t, lib, path), opA), intentRead(t, op, idA))
}

func TestAFinishedOperationIsReadBackExactlyAfterAReopen(t *testing.T) {
	lib, path := openLibrary(t)
	finished := time.Date(2026, 11, 4, 12, 0, 1, 42, time.FixedZone("CET", 3600))
	cause := errors.New("disk I/O error in /Users/owner/Library/library.sqlite")
	diskFull := contracts.NewAppError(contracts.CodeLibraryDiskFull, nil).WithCause(cause)
	invalid := contracts.InvalidRequest("selectedFileIndexes[0]", contracts.ReasonRange).WithOperation(opA).WithTorrent(idA)
	stored := func(p *contracts.AppError) *contracts.AppError {
		// What the journal keeps: the code, IDs and parameters, never the cause.
		return &contracts.AppError{Code: p.Code, OperationID: p.OperationID, TorrentID: p.TorrentID, Params: p.Params}
	}
	for i, c := range []struct {
		state   contracts.OperationState
		problem *contracts.AppError
		outcome *contracts.AppError
	}{
		{contracts.OperationSucceeded, nil, nil},
		{contracts.OperationFailed, diskFull, invalid},
		// An error built without NewAppError has no params; the journal stores {} all the same.
		{contracts.OperationCancelled, &contracts.AppError{Code: contracts.CodeOperationCancelled}, nil},
	} {
		torrent, op := newID(t), importIntent(newID(t), newID(t))
		add := sampleTorrent(torrent, byte(i+1))
		if err := lib.AddTorrent(deadline(t, 5*time.Second), add, op); err != nil {
			t.Fatal(err)
		}
		end := OperationEnd{State: c.state, Error: c.problem, FinishedAt: finished,
			Outcomes: map[contracts.TorrentID]*contracts.AppError{contracts.TorrentID(torrent): c.outcome}}
		if err := lib.FinishOperation(deadline(t, 5*time.Second), op.ID, end); err != nil {
			t.Fatalf("FinishOperation(%s): %v", c.state, err)
		}
		want := intentRead(t, op, contracts.TorrentID(torrent))
		// A finished operation keeps no request, so no path of it outlives it; its fingerprint still
		// tells a repeat.
		want.State, want.FinishedAt, want.Request = c.state, finished, json.RawMessage("{}")
		if c.problem != nil {
			want.Error = stored(c.problem)
			if want.Error.Params == nil {
				want.Error.Params = map[string]string{}
			}
		}
		if c.outcome != nil {
			want.Torrents[0].Outcome = stored(c.outcome)
		}
		lib = reopen(t, lib, path)
		got := operation(t, lib, op.ID)
		sameOperation(t, got, want)
		if got.Error != nil && errors.Unwrap(got.Error) != nil {
			t.Errorf("the journal kept the cause of %s", got.Error.Code)
		}
		var exists *OperationExistsError
		if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(newID(t), byte(i+0x40)), op); !errors.As(err, &exists) || !exists.SameRequest {
			t.Errorf("a repeat of finished operation %s = %v, want ErrOperationExists with the same request", op.ID, err)
		}
	}
}

func TestAnInvalidEndIsRefusedAndChangesNothing(t *testing.T) {
	lib, _ := openLibrary(t)
	op := importIntent(opA, newID(t))
	if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idA, 1), op); err != nil {
		t.Fatal(err)
	}
	ok := OperationEnd{State: contracts.OperationSucceeded, FinishedAt: time.Now()}
	failed := contracts.NewAppError(contracts.CodeInternalUnexpected, nil)
	for name, c := range map[string]struct {
		id  contracts.OperationID
		end func(*OperationEnd)
	}{
		"an operation ID that is not a UUID":  {"op", func(*OperationEnd) {}},
		"an operation the journal lacks":      {idB, func(*OperationEnd) {}},
		"a state that is not final":           {opA, func(e *OperationEnd) { e.State = contracts.OperationRunning }},
		"a state outside the contract":        {opA, func(e *OperationEnd) { e.State = "done" }},
		"success with an error":               {opA, func(e *OperationEnd) { e.Error = failed }},
		"a failure without an error":          {opA, func(e *OperationEnd) { e.State = contracts.OperationFailed }},
		"an error without a code":             {opA, func(e *OperationEnd) { e.State, e.Error = contracts.OperationFailed, &contracts.AppError{} }},
		"an outcome without a code":           {opA, func(e *OperationEnd) { e.Outcomes = map[contracts.TorrentID]*contracts.AppError{idA: {}} }},
		"an outcome of a torrent not named":   {opA, func(e *OperationEnd) { e.Outcomes = map[contracts.TorrentID]*contracts.AppError{idB: nil} }},
		"an outcome of an ID that is no UUID": {opA, func(e *OperationEnd) { e.Outcomes = map[contracts.TorrentID]*contracts.AppError{"x": nil} }},
		"no time":                             {opA, func(e *OperationEnd) { e.FinishedAt = time.Time{} }},
	} {
		end := ok
		c.end(&end)
		if err := lib.FinishOperation(deadline(t, 5*time.Second), c.id, end); !errors.Is(err, ErrInvalid) {
			t.Errorf("FinishOperation with %s = %v, want ErrInvalid", name, err)
		}
	}
	sameOperation(t, operation(t, lib, opA), intentRead(t, op, idA))
}

func TestAFinishedOperationNeverFinishesAgain(t *testing.T) {
	lib, _ := openLibrary(t)
	op := importIntent(opA, newID(t))
	if err := lib.AddTorrent(deadline(t, 5*time.Second), sampleTorrent(idA, 1), op); err != nil {
		t.Fatal(err)
	}
	finished := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if err := lib.FinishOperation(deadline(t, 5*time.Second), opA, OperationEnd{State: contracts.OperationSucceeded, FinishedAt: finished}); err != nil {
		t.Fatal(err)
	}
	again := OperationEnd{State: contracts.OperationFailed, Error: contracts.NewAppError(contracts.CodeInternalUnexpected, nil), FinishedAt: finished.Add(time.Second)}
	if err := lib.FinishOperation(deadline(t, 5*time.Second), opA, again); !errors.Is(err, ErrInvalid) {
		t.Fatalf("finishing a finished operation = %v, want ErrInvalid", err)
	}
	want := intentRead(t, op, idA)
	want.State, want.FinishedAt, want.Request = contracts.OperationSucceeded, finished, json.RawMessage("{}")
	sameOperation(t, operation(t, lib, opA), want)
}

func TestAnOperationTheJournalLacksIsNotFound(t *testing.T) {
	lib, _ := openLibrary(t)
	if op, found, err := lib.Operation(deadline(t, 5*time.Second), opA); found || err != nil || !reflect.DeepEqual(op, Operation{}) {
		t.Fatalf("Operation of an empty journal = %+v, %v, %v", op, found, err)
	}
	if _, _, err := lib.Operation(deadline(t, 5*time.Second), "op"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Operation of an ID that is not a UUID = %v, want ErrInvalid", err)
	}
}
