package operations

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"octopustorrent/internal/app/contracts"
)

const (
	runtimeID = contracts.RuntimeID("8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e")
	opID      = contracts.OperationID("c0ffee00-1234-4abc-9def-0123456789ab")
	kind      = contracts.OperationPauseTorrents
)

// wait lets every recorded operation of l finish, then ends its goroutines, so final states can be
// read. Shutting down first would cancel operations that had not started yet.
func wait(t *testing.T, l *Ledger) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !allFinished(l); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("operations did not finish")
		}
	}
	if err := l.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func allFinished(l *Ledger) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if !e.status.State.Finished() {
			return false
		}
	}
	return true
}

func get(t *testing.T, l *Ledger, id contracts.OperationID) contracts.OperationStatus {
	t.Helper()
	status, problem := l.Get(id)
	if problem != nil {
		t.Fatalf("Get(%s): %+v", id, problem)
	}
	return status
}

func TestAcceptedOperationRunsAndItsResultCanBeReadAgain(t *testing.T) {
	l := NewLedger(runtimeID)
	release := make(chan struct{})
	var runs atomic.Int32
	accepted, problem := l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError {
		runs.Add(1)
		<-release
		return nil
	})
	want := contracts.OperationStatus{OperationID: opID, RuntimeID: runtimeID, Kind: kind, State: contracts.OperationAccepted}
	if problem != nil || accepted != want {
		t.Fatalf("Start = %+v, %+v; want %+v", accepted, problem, want)
	}
	close(release)
	wait(t, l)
	want.State = contracts.OperationSucceeded
	if got := get(t, l, opID); got != want {
		t.Fatalf("after completion Get = %+v, want %+v", got, want)
	}
	if runs.Load() != 1 {
		t.Fatalf("the mutation ran %d times", runs.Load())
	}
}

func TestOperationIsRunningWhileItsMutationRuns(t *testing.T) {
	l := NewLedger(runtimeID)
	started, release := make(chan struct{}), make(chan struct{})
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError {
		close(started)
		<-release
		return nil
	})
	<-started
	if got := get(t, l, opID).State; got != contracts.OperationRunning {
		t.Fatalf("state = %s, want running", got)
	}
	close(release)
	wait(t, l)
}

// A retry after a lost response repeats the operation ID with the same request: it gets the
// stored status and the mutation does not run again, even when the retries race.
func TestRepeatedOperationIDWithTheSameRequestRunsOnce(t *testing.T) {
	l := NewLedger(runtimeID)
	var runs atomic.Int32
	mutation := func(context.Context) *contracts.AppError {
		runs.Add(1)
		return nil
	}
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			if _, problem := l.Start(opID, kind, "fp", mutation); problem != nil {
				t.Errorf("duplicate Start: %+v", problem)
			}
		})
	}
	group.Wait()
	wait(t, l)
	if runs.Load() != 1 {
		t.Fatalf("the mutation ran %d times, want 1", runs.Load())
	}
	again, problem := l.Start(opID, kind, "fp", mutation)
	if problem != nil || again.State != contracts.OperationSucceeded {
		t.Fatalf("Start after completion = %+v, %+v; want the stored success", again, problem)
	}
}

func TestOperationIDReusedForAnotherRequestIsAConflict(t *testing.T) {
	l := NewLedger(runtimeID)
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError { return nil })
	for _, attempt := range []struct {
		kind        contracts.OperationKind
		fingerprint string
	}{{kind, "other"}, {contracts.OperationResumeTorrents, "fp"}} {
		_, problem := l.Start(opID, attempt.kind, attempt.fingerprint, func(context.Context) *contracts.AppError {
			t.Error("a conflicting request ran")
			return nil
		})
		if problem == nil || problem.Code != contracts.CodeOperationIDConflict || *problem.OperationID != opID {
			t.Errorf("Start(%v) = %+v, want operation.idConflict", attempt, problem)
		}
	}
	wait(t, l)
}

func TestFailedOperationKeepsItsErrorWithTheOperationID(t *testing.T) {
	l := NewLedger(runtimeID)
	failure := contracts.NewAppError("torrent.notFound", nil)
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError { return failure })
	wait(t, l)
	got := get(t, l, opID)
	if got.State != contracts.OperationFailed || got.Error.Code != "torrent.notFound" || *got.Error.OperationID != opID {
		t.Fatalf("Get = %+v (error %+v), want failed with the mutation's error", got, got.Error)
	}
	got.Error.Params["added"] = "x"
	if failure.OperationID != nil || len(failure.Params) != 0 {
		t.Error("the ledger shares or changed the error the mutation returned")
	}
}

func TestCancelBeforeTheMutationTookEffectCancelsIt(t *testing.T) {
	l := NewLedger(runtimeID)
	started := make(chan struct{})
	l.Start(opID, kind, "fp", func(ctx context.Context) *contracts.AppError {
		close(started)
		<-ctx.Done()
		return contracts.NewAppError(contracts.CodeOperationCancelled, nil)
	})
	<-started
	if _, problem := l.Cancel(opID); problem != nil {
		t.Fatalf("Cancel: %+v", problem)
	}
	wait(t, l)
	got := get(t, l, opID)
	if got.State != contracts.OperationCancelled || got.Error.Code != contracts.CodeOperationCancelled {
		t.Fatalf("Get = %+v, want cancelled", got)
	}
}

// An operation cancelled before its goroutine ran never runs its mutation: no mutation needs its
// own check of the context before it starts.
func TestCancelBeforeTheMutationStartedSkipsIt(t *testing.T) {
	l := NewLedger(runtimeID)
	release := make(chan struct{})
	start := l.spawn
	l.spawn = func(operation func()) {
		start(func() {
			<-release
			operation()
		})
	}
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError {
		t.Error("a cancelled operation ran its mutation")
		return nil
	})
	l.Cancel(opID)
	close(release)
	wait(t, l)
	got := get(t, l, opID)
	if got.State != contracts.OperationCancelled || got.Error.Code != contracts.CodeOperationCancelled || *got.Error.OperationID != opID {
		t.Fatalf("Get = %+v, want cancelled without running", got)
	}
}

// Cancelling an operation that already committed changes nothing: its final state stays.
func TestCancelAfterCompletionKeepsTheFinalState(t *testing.T) {
	l := NewLedger(runtimeID)
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError { return nil })
	wait(t, l)
	status, problem := l.Cancel(opID)
	if problem != nil || status.State != contracts.OperationSucceeded {
		t.Fatalf("Cancel = %+v, %+v; want the stored success", status, problem)
	}
}

// After a restart the ledger is empty; the answer names this runtime so the frontend can tell
// that the outcome of an operation accepted by another runtime is unknown.
func TestUnknownOperationIsNotFoundInThisRuntime(t *testing.T) {
	l := NewLedger(runtimeID)
	for name, call := range map[string]func(contracts.OperationID) (contracts.OperationStatus, *contracts.AppError){
		"Get": l.Get, "Cancel": l.Cancel,
	} {
		_, problem := call(opID)
		if problem == nil || problem.Code != contracts.CodeOperationNotFound ||
			problem.Params["runtimeID"] != string(runtimeID) || *problem.OperationID != opID {
			t.Errorf("%s = %+v, want operation.notFound for this runtime", name, problem)
		}
	}
	if l.RuntimeID() != runtimeID {
		t.Errorf("RuntimeID() = %s", l.RuntimeID())
	}
}

func TestShutdownCancelsRunningMutationsAndWaitsForThem(t *testing.T) {
	l := NewLedger(runtimeID)
	started := make(chan struct{})
	var ended atomic.Bool
	l.Start(opID, kind, "fp", func(ctx context.Context) *contracts.AppError {
		close(started)
		<-ctx.Done()
		ended.Store(true)
		return contracts.NewAppError(contracts.CodeOperationCancelled, nil)
	})
	<-started
	if err := l.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !ended.Load() {
		t.Fatal("Shutdown returned before the mutation ended")
	}
	if got := get(t, l, opID).State; got != contracts.OperationCancelled {
		t.Fatalf("state after Shutdown = %s, want cancelled", got)
	}
}

// A mutation that ignores cancellation cannot hold the quit forever: Shutdown gives up when its
// context ends and says so.
func TestShutdownReportsMutationsStillRunningWhenItsContextEnds(t *testing.T) {
	l := NewLedger(runtimeID)
	started, release := make(chan struct{}), make(chan struct{})
	l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError {
		close(started)
		<-release
		return nil
	})
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := l.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want the context's deadline error", err)
	}
	close(release)
	wait(t, l)
}

func TestNoOperationStartsAfterShutdown(t *testing.T) {
	l := NewLedger(runtimeID)
	wait(t, l)
	_, problem := l.Start(opID, kind, "fp", func(context.Context) *contracts.AppError {
		t.Error("a mutation ran after Shutdown")
		return nil
	})
	if problem == nil || problem.Code != contracts.CodeOperationCancelled || *problem.OperationID != opID {
		t.Fatalf("Start after Shutdown = %+v, want operation.cancelled", problem)
	}
	if _, problem := l.Get(opID); problem == nil {
		t.Error("a refused operation was recorded")
	}
}
