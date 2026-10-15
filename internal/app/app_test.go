package app

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
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
	return New(runtimeID, ledger), ledger
}

func TestGetRuntimeInfoReportsCompiledGoRuntime(t *testing.T) {
	a, _ := newApp()
	info := a.GetRuntimeInfo()

	want := RuntimeInfo{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
	if info != want {
		t.Fatalf("GetRuntimeInfo() = %+v, want %+v", info, want)
	}
}

func TestRuntimeInfoJSONUsesBridgeFieldNames(t *testing.T) {
	encoded, err := json.Marshal(RuntimeInfo{GoVersion: "go1.0", OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	const want = `{"goVersion":"go1.0","os":"darwin","arch":"arm64"}`
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
	ledger.Start(opID, contracts.OperationPauseTorrents, "fp", func(context.Context) *contracts.AppError { return nil })
	if err := ledger.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	status, err := a.GetOperation(opID)
	if err != nil || status.State != contracts.OperationSucceeded || status.OperationID != opID {
		t.Fatalf("GetOperation = %+v, %v", status, err)
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
// JSON text of the error; anything without a code of its own becomes internal.unexpected.
func TestFormatErrorSendsAppErrorsAsJSONText(t *testing.T) {
	problem := contracts.NewAppError(contracts.CodeOperationNotFound, map[string]string{"runtimeID": string(runtimeID)}).WithOperation(opID)
	for err, want := range map[error]string{
		problem:                    `{"code":"operation.notFound","operationID":"` + opID + `","torrentID":null,"params":{"runtimeID":"` + string(runtimeID) + `"}}`,
		errors.New("disk on fire"): `{"code":"internal.unexpected","operationID":null,"torrentID":null,"params":{}}`,
	} {
		if got := FormatError(err); got != want {
			t.Errorf("FormatError(%v) = %v, want %s", err, got, want)
		}
	}
}
