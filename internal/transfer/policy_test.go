package transfer

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

// The decision tables below are a second encoding of the policy: ordered rows with wildcards, the
// first matching row wins. Every combination of inputs is checked against them, every row must be
// the first match of some combination, and their markdown rendering must be the block between
// markers in the ADR, so the ADR, the tests and the code state one table.

const adrPath = "../../docs/decisions/transfer-status-model.md"

// cond is a table cell over a boolean input.
type cond int

const (
	anyValue cond = iota
	yes
	no
)

func (c cond) matches(b bool) bool { return c == anyValue || (c == yes) == b }

func (c cond) String() string { return [...]string{"any", "yes", "no"}[c] }

// phaseNone is the zero value, a phase nobody set; phaseUnknown stands for a value outside the
// declared phases. Neither may ever transfer.
const (
	phaseNone    Phase = 0
	phaseUnknown Phase = 99
)

var allPhases = []Phase{PhaseReady, PhaseWaitingForMetadata, PhaseChecking, PhaseRelinking, PhaseRemoving, phaseNone, phaseUnknown}

var phaseNames = map[Phase]string{
	PhaseReady: "ready", PhaseWaitingForMetadata: "waiting for metadata", PhaseChecking: "checking",
	PhaseRelinking: "relinking", PhaseRemoving: "removing", phaseNone: "none (zero)", phaseUnknown: "unknown",
}

// phases is a table cell over the phase: nil matches any phase.
type phases []Phase

var (
	anyPhase = phases(nil)
	ready    = phases{PhaseReady}
	notReady = phases{PhaseWaitingForMetadata, PhaseChecking, PhaseRelinking, PhaseRemoving, phaseNone, phaseUnknown}
)

func (p phases) matches(phase Phase) bool { return p == nil || slices.Contains(p, phase) }

func (p phases) String() string {
	if p == nil {
		return "any"
	}
	names := make([]string, len(p))
	for i, phase := range p {
		names[i] = phaseNames[phase]
	}
	if slices.Equal(p, notReady) {
		return "not ready: " + strings.Join(names, ", ")
	}
	return strings.Join(names, ", ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

type permissionRow struct {
	shutdown cond
	phase    phases
	fault    cond
	paused   cond
	global   cond
	want     Permissions
}

var permissionTable = []permissionRow{
	{shutdown: yes, phase: anyPhase, fault: anyValue, paused: anyValue, global: anyValue, want: Permissions{}},
	{shutdown: no, phase: notReady, fault: anyValue, paused: anyValue, global: anyValue, want: Permissions{}},
	{shutdown: no, phase: ready, fault: yes, paused: anyValue, global: anyValue, want: Permissions{}},
	{shutdown: no, phase: ready, fault: no, paused: yes, global: anyValue, want: Permissions{}},
	{shutdown: no, phase: ready, fault: no, paused: no, global: yes, want: Permissions{Download: true}},
	{shutdown: no, phase: ready, fault: no, paused: no, global: no, want: Permissions{Download: true, Upload: true}},
}

func (r permissionRow) matches(in Input) bool {
	return r.shutdown.matches(in.ShuttingDown) && r.phase.matches(in.Phase) && r.fault.matches(in.PayloadFault) &&
		r.paused.matches(in.ManualPaused) && r.global.matches(in.GlobalUploadDisabled)
}

func renderPermissionTable() string {
	var b strings.Builder
	b.WriteString("| # | Shutting down | Phase | Payload fault | Manual pause | Global upload stop | Download | Upload |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for i, r := range permissionTable {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | %s |\n", i+1, r.shutdown, r.phase, r.fault, r.paused, r.global,
			yesNo(r.want.Download), yesNo(r.want.Upload))
	}
	return b.String()
}

// allInputs is every combination of the inputs of Decide, unknown phase included.
func allInputs() []Input {
	var inputs []Input
	for _, phase := range allPhases {
		for _, shutdown := range []bool{false, true} {
			for _, fault := range []bool{false, true} {
				for _, paused := range []bool{false, true} {
					for _, global := range []bool{false, true} {
						inputs = append(inputs, Input{ManualPaused: paused, GlobalUploadDisabled: global, Phase: phase, PayloadFault: fault, ShuttingDown: shutdown})
					}
				}
			}
		}
	}
	return inputs
}

func TestDecideFollowsThePermissionTable(t *testing.T) {
	firstMatches := make([]int, len(permissionTable))
	for _, in := range allInputs() {
		row := slices.IndexFunc(permissionTable, func(r permissionRow) bool { return r.matches(in) })
		if row < 0 {
			t.Fatalf("no row of the table covers %+v", in)
		}
		firstMatches[row]++
		if got := Decide(in); got != permissionTable[row].want {
			t.Errorf("Decide(%+v) = %+v, row %d says %+v", in, got, row+1, permissionTable[row].want)
		}
	}
	for row, n := range firstMatches {
		if n == 0 {
			t.Errorf("row %d is never the first match: it is dead", row+1)
		}
	}
}

// The requirements, each checked over every combination, independently of the table's rows.
func TestDecideKeepsTheRequirementsForEveryInput(t *testing.T) {
	for _, in := range allInputs() {
		got := Decide(in)
		flipped := in
		flipped.GlobalUploadDisabled = !in.GlobalUploadDisabled
		resumed := in
		resumed.ManualPaused = false
		reenabled := in
		reenabled.GlobalUploadDisabled = false
		switch {
		case in.ManualPaused && got != (Permissions{}):
			t.Errorf("TR-01: a manual pause must stop both directions: %+v → %+v", in, got)
		case in.GlobalUploadDisabled && got.Upload:
			t.Errorf("TR-08: the global stop must stop upload: %+v → %+v", in, got)
		case Decide(flipped).Download != got.Download:
			t.Errorf("TR-08: the global stop must never change download: %+v", in)
		case in.GlobalUploadDisabled && Decide(resumed).Upload:
			t.Errorf("resuming must not cancel the global upload ban: %+v", in)
		case in.ManualPaused && Decide(reenabled) != (Permissions{}):
			t.Errorf("re-enabling global upload must not start a paused torrent: %+v", in)
		case in.Phase == PhaseReady && !in.PayloadFault && !in.ManualPaused && !in.ShuttingDown && !in.GlobalUploadDisabled && !got.Upload:
			t.Errorf("TR-03: a ready torrent nobody stopped must seed: %+v", in)
		case (in.ShuttingDown || in.PayloadFault || in.Phase != PhaseReady) && got != (Permissions{}):
			t.Errorf("shutdown, a fault and every phase but ready stop both directions: %+v → %+v", in, got)
		}
	}
}

// adrBlock returns the text between <!-- name:begin --> and <!-- name:end --> in the ADR.
func adrBlock(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(adrPath)
	if err != nil {
		t.Fatalf("read the ADR: %v", err)
	}
	_, after, found := strings.Cut(string(data), "<!-- "+name+":begin -->\n")
	block, _, closed := strings.Cut(after, "<!-- "+name+":end -->")
	if !found || !closed {
		t.Fatalf("the ADR has no %s block", name)
	}
	return block
}

// A field nobody set must not let a torrent transfer (security review M4): the zero input stops
// both directions, because the zero phase is not PhaseReady.
func TestTheZeroInputTransfersNothing(t *testing.T) {
	if got := Decide(Input{}); got != (Permissions{}) {
		t.Fatalf("Decide(Input{}) = %+v, want nothing", got)
	}
}

func TestTheADRStatesThePermissionTable(t *testing.T) {
	if got, want := adrBlock(t, "transfer-permissions"), renderPermissionTable(); got != want {
		t.Fatalf("the ADR's permission table differs from the tested one.\nADR:\n%s\ntests:\n%s", got, want)
	}
}
