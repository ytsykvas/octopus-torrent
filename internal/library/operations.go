package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"octopustorrent/internal/app/contracts"
)

// OperationIntent is an accepted non-trivial operation as the journal records it when its mutation
// starts: by which run, of which kind, for which request. The journal outlives the torrents it
// names, for recovery and for a repeated operation ID.
type OperationIntent struct {
	// ID is the request's own operationID: the journal keys the entry by it, and a repeat is found
	// by it.
	ID        contracts.OperationID
	RuntimeID contracts.RuntimeID
	Kind      contracts.OperationKind
	// Request is what the frontend asked for; the journal stores its JSON while the operation runs
	// (for recovery after a crash) and its fingerprint for good.
	Request    contracts.Request
	AcceptedAt time.Time
}

// check returns ErrInvalid for an intent the journal must not store. SQLite checks only that a kind
// is not empty (task 014, owner decision), so the kind is checked against the contract here.
func (op OperationIntent) check() error {
	switch {
	case !contracts.IsUUID(string(op.ID)):
		return invalid("operation ID %q is not a UUID", op.ID)
	case !contracts.IsUUID(string(op.RuntimeID)):
		return invalid("operation %s has the runtime ID %q, not a UUID", op.ID, op.RuntimeID)
	case !op.Kind.Valid():
		return invalid("operation %s has the kind %q", op.ID, op.Kind)
	// Every request is a pointer to a struct; a nil one has nothing to record.
	case op.Request == nil || reflect.ValueOf(op.Request).IsNil():
		return invalid("operation %s has no request", op.ID)
	case !storable(op.AcceptedAt):
		return invalid("operation %s was accepted at %v, which the library cannot store", op.ID, op.AcceptedAt)
	}
	return nil
}

// inserts are the statements that record the intent as running, naming the torrents.
func (op OperationIntent) inserts(torrents ...contracts.TorrentID) []statement {
	request, _ := json.Marshal(op.Request) // a request is plain structs of strings, numbers and slices
	out := []statement{{
		`INSERT INTO operations(operation_id, runtime_id, kind, fingerprint, request, state, accepted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
		[]any{op.ID, op.RuntimeID, op.Kind, contracts.Fingerprint(op.Request), string(request),
			contracts.OperationRunning, op.AcceptedAt.UnixNano()},
	}}
	for _, id := range torrents {
		out = append(out, statement{"INSERT INTO operation_torrents(operation_id, torrent_id) VALUES (?, ?)", []any{op.ID, id}})
	}
	return out
}

// Operation is an operation as the journal holds it.
type Operation struct {
	ID          contracts.OperationID
	RuntimeID   contracts.RuntimeID
	Kind        contracts.OperationKind
	Fingerprint string
	// Request is the JSON of the request it was accepted for while it runs, and {} once it finished.
	Request json.RawMessage
	State   contracts.OperationState
	// Error is set exactly when State is failed or cancelled; the journal keeps no cause.
	Error      *contracts.AppError
	AcceptedAt time.Time
	// FinishedAt is zero until the operation finished.
	FinishedAt time.Time
	// Torrents are the torrents it names, in ID order.
	Torrents []OperationTorrent
}

// OperationTorrent is one torrent an operation names, with its own outcome once the operation
// finished: nil when it succeeded (or before the end), the torrent's error otherwise.
type OperationTorrent struct {
	ID      contracts.TorrentID
	Outcome *contracts.AppError
}

// OperationEnd is how an operation finished: its final state, its error (set exactly when it failed
// or was cancelled), the outcomes of the torrents it names that have one, and when.
type OperationEnd struct {
	State      contracts.OperationState
	Error      *contracts.AppError
	Outcomes   map[contracts.TorrentID]*contracts.AppError
	FinishedAt time.Time
}

// check returns ErrInvalid for an end the journal must not store.
func (e OperationEnd) check(id contracts.OperationID) error {
	failed := e.State == contracts.OperationFailed || e.State == contracts.OperationCancelled
	switch {
	case !contracts.IsUUID(string(id)):
		return invalid("operation ID %q is not a UUID", id)
	case !e.State.Finished():
		return invalid("operation %s cannot end in the state %q", id, e.State)
	case failed != (e.Error != nil):
		return invalid("operation %s ends %s with the error %v", id, e.State, e.Error)
	case e.Error != nil && e.Error.Code == "":
		return invalid("operation %s ends with an error without a code", id)
	case !storable(e.FinishedAt):
		return invalid("operation %s finished at %v, which the library cannot store", id, e.FinishedAt)
	}
	for torrent, outcome := range e.Outcomes {
		switch {
		case !contracts.IsUUID(string(torrent)):
			return invalid("operation %s has an outcome for %q, not a UUID", id, torrent)
		case outcome != nil && outcome.Code == "":
			return invalid("operation %s has an outcome without a code for %s", id, torrent)
		}
	}
	return nil
}

// FinishOperation records how a running operation ended, in one transaction with the outcomes of
// its torrents, and drops its request: a finished operation needs none (its fingerprint tells a
// repeat), and the request's paths (a destination, a default folder) must not outlive it. An
// operation the journal lacks, one that already finished (states only move forward), or an outcome
// for a torrent it does not name is ErrInvalid, and nothing changes.
func (l *Library) FinishOperation(ctx context.Context, id contracts.OperationID, end OperationEnd) error {
	if err := end.check(id); err != nil {
		return err
	}
	return l.store.Write(ctx, func(tx txn) error {
		result, err := tx.Exec("UPDATE operations SET state = ?, error = ?, finished_at = ?, request = '{}' WHERE operation_id = ? AND finished_at IS NULL",
			end.State, problemJSON(end.Error), end.FinishedAt.UnixNano(), id)
		if err != nil {
			return err
		}
		// modernc's RowsAffected never fails (result.go).
		if n, _ := result.RowsAffected(); n == 0 {
			return invalid("the journal has no running operation %s", id)
		}
		for torrent, outcome := range end.Outcomes {
			result, err := tx.Exec("UPDATE operation_torrents SET outcome = ? WHERE operation_id = ? AND torrent_id = ?",
				problemJSON(outcome), id, torrent)
			if err != nil {
				return err
			}
			if n, _ := result.RowsAffected(); n == 0 {
				return invalid("operation %s does not name torrent %s", id, torrent)
			}
		}
		return nil
	})
}

// Operation returns the journal's entry of the operation; found is false when the journal lacks it.
func (l *Library) Operation(ctx context.Context, id contracts.OperationID) (op Operation, found bool, err error) {
	if !contracts.IsUUID(string(id)) {
		return Operation{}, false, invalid("operation ID %q is not a UUID", id)
	}
	err = l.store.Read(ctx, func(tx txn) error {
		var request string
		var problem sql.NullString
		var accepted int64
		var finished sql.NullInt64
		err := tx.QueryRow(`SELECT runtime_id, kind, fingerprint, request, state, error, accepted_at, finished_at
			FROM operations WHERE operation_id = ?`, id).Scan(&op.RuntimeID, &op.Kind, &op.Fingerprint, &request,
			&op.State, &problem, &accepted, &finished)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		op.ID, op.Request, op.AcceptedAt = id, json.RawMessage(request), instant(accepted)
		if finished.Valid {
			op.FinishedAt = instant(finished.Int64)
		}
		if op.Error, err = storedProblem(problem); err != nil {
			return err
		}
		return queryEach(tx, func(rows *sql.Rows) error {
			var t OperationTorrent
			var outcome sql.NullString
			if err := rows.Scan(&t.ID, &outcome); err != nil {
				return err
			}
			var err error
			if t.Outcome, err = storedProblem(outcome); err != nil {
				return err
			}
			op.Torrents = append(op.Torrents, t)
			return nil
		}, "SELECT torrent_id, outcome FROM operation_torrents WHERE operation_id = ? ORDER BY torrent_id", id)
	})
	if err != nil || !found {
		return Operation{}, false, err
	}
	return op, true, nil
}

// storedProblem is the AppError the journal stored as JSON, nil for NULL.
func storedProblem(stored sql.NullString) (*contracts.AppError, error) {
	if !stored.Valid {
		return nil, nil
	}
	var p contracts.AppError
	if err := json.Unmarshal([]byte(stored.String), &p); err != nil {
		return nil, err
	}
	return &p, nil
}
