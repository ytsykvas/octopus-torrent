package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"octopustorrent/internal/app/contracts"
	"octopustorrent/internal/app/operations"
)

const (
	runtimeID = contracts.RuntimeID("8b1c2d3e-4f5a-4b6c-8d7e-9f0a1b2c3d4e")
	opID      = "c0ffee00-1234-4abc-9def-0123456789ab"
)

func newApp() (*App, *operations.Ledger) {
	ledger := operations.NewLedger(runtimeID)
	return New(ledger, "3.53.4"), ledger
}

func TestGetRuntimeInfoReportsCompiledGoRuntime(t *testing.T) {
	a, _ := newApp()
	info := a.GetRuntimeInfo()

	want := RuntimeInfo{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, SQLiteVersion: "3.53.4"}
	if info != want {
		t.Fatalf("GetRuntimeInfo() = %+v, want %+v", info, want)
	}
}

func TestRuntimeInfoJSONUsesBridgeFieldNames(t *testing.T) {
	encoded, err := json.Marshal(RuntimeInfo{GoVersion: "go1.0", OS: "darwin", Arch: "arm64", SQLiteVersion: "3.53.4"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	const want = `{"goVersion":"go1.0","os":"darwin","arch":"arm64","sqliteVersion":"3.53.4"}`
	if string(encoded) != want {
		t.Fatalf("JSON = %s, want %s", encoded, want)
	}
}

// Until the library exists (task 034) the snapshot truthfully says: this runtime, revision 0,
// no torrents — an empty list, never null.
func TestGetLibrarySnapshotDescribesThisRuntimesEmptyLibrary(t *testing.T) {
	a, _ := newApp()
	encoded, err := json.Marshal(a.GetLibrarySnapshot())
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"runtimeID":"` + string(runtimeID) + `","revision":"0","torrents":[]}`
	if string(encoded) != want {
		t.Fatalf("GetLibrarySnapshot() = %s, want %s", encoded, want)
	}
}

func TestGetOperationReturnsTheLedgersStatus(t *testing.T) {
	a, ledger := newApp()
	ran := make(chan struct{})
	ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError {
		close(ran)
		return nil
	})
	<-ran
	// Shutdown waits for the mutation, which reaches succeeded before its goroutine ends.
	if err := ledger.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status, err := a.GetOperation(opID); err != nil || status.OperationID != opID || status.State != contracts.OperationSucceeded {
		t.Fatalf("GetOperation = %+v, %v; want the succeeded operation", status, err)
	}
}

func TestGetOperationRejectsBadAndUnknownIDsWithAppErrors(t *testing.T) {
	a, _ := newApp()
	for id, want := range map[string]map[string]string{
		"":                        {"field": "operationID", "reason": contracts.ReasonMissing},
		"C0FFEE00-1234-4ABC-9DEF": {"field": "operationID", "reason": contracts.ReasonFormat},
		opID:                      {"runtimeID": string(runtimeID)},
	} {
		_, err := a.GetOperation(id)
		var problem *contracts.AppError
		if !errors.As(err, &problem) {
			t.Fatalf("GetOperation(%q) error = %v, want an AppError", id, err)
		}
		for key, value := range want {
			if problem.Params[key] != value {
				t.Errorf("GetOperation(%q) = %+v, want params %v", id, problem, want)
			}
		}
	}
}

// Wails rejects the JavaScript promise with new Error(formatted), so the AppError crosses as the
// JSON text of the error; anything without a code of its own becomes internal.unexpected, and its
// cause goes to the local diagnostics output instead of the UI.
func TestErrorFormatterSendsAppErrorsAsJSONText(t *testing.T) {
	var diagnostics bytes.Buffer
	format := ErrorFormatter(&diagnostics)
	problem := contracts.NewAppError(contracts.CodeOperationNotFound, map[string]string{"runtimeID": string(runtimeID)}).WithOperation(opID)

	if got, want := format(problem), `{"code":"operation.notFound","operationID":"`+opID+`","torrentID":null,"params":{"runtimeID":"`+string(runtimeID)+`"}}`; got != want {
		t.Errorf("format(AppError) = %v, want %s", got, want)
	}
	if diagnostics.Len() != 0 {
		t.Errorf("an AppError was logged: %q", diagnostics.String())
	}
	if got, want := format(errors.New("disk on fire")), `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}`; got != want {
		t.Errorf("format(other error) = %v, want %s", got, want)
	}
	if want := "OctopusTorrent: unexpected error: \"disk on fire\"\n"; diagnostics.String() != want {
		t.Errorf("diagnostics = %q, want %q", diagnostics.String(), want)
	}
	diagnostics.Reset()
	if got := format(errors.New("forged\nOctopusTorrent: line")); got != `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}` {
		t.Errorf("format(error with a newline) = %v", got)
	}
	if strings.Count(diagnostics.String(), "\n") != 1 {
		t.Errorf("a newline in an error forged a diagnostics line: %q", diagnostics.String())
	}
	var typedNil *contracts.AppError
	if got := format(typedNil); got != `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}` {
		t.Errorf("format(typed nil AppError) = %v", got)
	}
}
