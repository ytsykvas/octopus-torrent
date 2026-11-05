package transfer

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"octopustorrent/internal/app/contracts"
)

// wanted is a table cell over the number of wanted files.
type wanted int

const (
	anyWanted wanted = iota
	noneWanted
	someWanted
)

func (w wanted) matches(files int) bool {
	return w == anyWanted || (w == noneWanted) == (files == 0)
}

func (w wanted) String() string { return [...]string{"any", "none", "at least one"}[w] }

type statusRow struct {
	phase    phases
	fault    cond
	paused   cond
	wanted   wanted
	complete cond
	global   cond
	want     contracts.TorrentStatus
}

var statusTable = []statusRow{
	{phase: phases{PhaseRelinking}, want: contracts.StatusRelinking},
	{phase: anyPhase, fault: yes, want: contracts.StatusError},
	{phase: phases{PhaseRemoving}, want: contracts.StatusDeleting},
	{phase: phases{PhaseChecking}, want: contracts.StatusChecking},
	{phase: phases{PhaseWaitingForMetadata}, want: contracts.StatusWaitingForMetadata},
	{phase: phases{phaseNone, phaseUnknown}, want: contracts.StatusError},
	{phase: ready, paused: yes, want: contracts.StatusPaused},
	{phase: ready, wanted: noneWanted, want: contracts.StatusNoFilesSelected},
	{phase: ready, complete: no, want: contracts.StatusDownloading},
	{phase: ready, global: no, want: contracts.StatusSeeding},
	{phase: ready, want: contracts.StatusCompleted},
}

func (r statusRow) matches(in Input, p Progress) bool {
	return r.phase.matches(in.Phase) && r.fault.matches(in.PayloadFault) && r.paused.matches(in.ManualPaused) &&
		r.wanted.matches(p.WantedFiles) && r.complete.matches(p.Complete) && r.global.matches(in.GlobalUploadDisabled)
}

func renderStatusTable() string {
	var b strings.Builder
	b.WriteString("| # | Phase | Payload fault | Manual pause | Wanted files | Complete | Global upload stop | Status |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|\n")
	for i, r := range statusTable {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | `%s` |\n", i+1, r.phase, r.fault, r.paused, r.wanted, r.complete, r.global, r.want)
	}
	return b.String()
}

type statusCase struct {
	in       Input
	progress Progress
}

func allStatusCases() []statusCase {
	var cases []statusCase
	for _, in := range allInputs() {
		for _, files := range []int{0, 2} {
			for _, complete := range []bool{false, true} {
				cases = append(cases, statusCase{in, Progress{WantedFiles: files, Complete: complete}})
			}
		}
	}
	return cases
}

func TestDisplayStatusFollowsTheStatusTable(t *testing.T) {
	firstMatches := make([]int, len(statusTable))
	for _, c := range allStatusCases() {
		row := slices.IndexFunc(statusTable, func(r statusRow) bool { return r.matches(c.in, c.progress) })
		if row < 0 {
			t.Fatalf("no row of the table covers %+v %+v", c.in, c.progress)
		}
		firstMatches[row]++
		if got := DisplayStatus(c.in, c.progress); got != statusTable[row].want {
			t.Errorf("DisplayStatus(%+v, %+v) = %s, row %d says %s", c.in, c.progress, got, row+1, statusTable[row].want)
		}
	}
	for row, n := range firstMatches {
		if n == 0 {
			t.Errorf("row %d is never the first match: it is dead", row+1)
		}
	}
}

// The status says what the policy allows, never more: it is checked against Decide, independently
// of the table's rows.
func TestDisplayStatusAgreesWithThePermissions(t *testing.T) {
	for _, c := range allStatusCases() {
		got := DisplayStatus(c.in, c.progress)
		running := c.in
		running.ShuttingDown = false
		allowed := Decide(running)
		quitting := c.in
		quitting.ShuttingDown = true
		switch {
		case DisplayStatus(quitting, c.progress) != got:
			t.Errorf("shutdown changed the status of %+v %+v", c.in, c.progress)
		case got == contracts.StatusPaused && !c.in.ManualPaused:
			t.Errorf("paused without a manual pause: %+v", c.in)
		case got == contracts.StatusDownloading && !allowed.Download:
			t.Errorf("downloading while download is not allowed: %+v %+v", c.in, c.progress)
		case got == contracts.StatusSeeding && !(allowed.Upload && c.progress.Complete && c.progress.WantedFiles > 0):
			t.Errorf("seeding without upload allowed and complete selected data: %+v %+v", c.in, c.progress)
		case got == contracts.StatusCompleted && (allowed.Upload || !c.progress.Complete):
			t.Errorf("completed while seeding is allowed or data is missing: %+v %+v", c.in, c.progress)
		case !slices.Contains(contracts.TorrentStatuses, got):
			t.Errorf("status %q is not in the vocabulary", got)
		}
	}
}

func TestTheADRStatesTheStatusTable(t *testing.T) {
	if got, want := adrBlock(t, "transfer-status"), renderStatusTable(); got != want {
		t.Fatalf("the ADR's status table differs from the tested one.\nADR:\n%s\ntests:\n%s", got, want)
	}
}
