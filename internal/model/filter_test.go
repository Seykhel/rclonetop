package model

import (
	"errors"
	"testing"
	"time"
)

func TestSubjectFilterMatchesLiteralPhraseAndKeepsSubjectOrder(t *testing.T) {
	s := NewState()
	s.Processes = []Process{
		{PID: 1, Kind: KindCopy, Paths: []string{"/Backup Casa/Foto"}, ReadRate: 3},
		{PID: 2, Kind: KindSync, Paths: []string{"/backup", "casa"}, ReadRate: 2},
		{PID: 3, Kind: KindCopy, Remotes: []string{"remote:Backup Casa"}, ReadRate: 1},
	}
	s.Units = []Unit{{Name: "backup casa.service"}, {Name: "other.service"}}
	full := s.Resolve()
	got := full.Filter("  BACKUP CASA\t")
	if len(got.Procs) != 2 || got.Procs[0].Process.PID != 1 || got.Procs[1].Process.PID != 3 || len(got.Units) != 1 {
		t.Fatalf("filter must preserve matching subjects in order: %+v", got)
	}
	if len(full.Procs) != 3 || len(full.Units) != 2 {
		t.Fatal("filter changed full view")
	}
	if all := full.Filter(" \t\n"); len(all.Procs) != 3 || len(all.Units) != 2 {
		t.Fatal("whitespace query must include all subjects")
	}
	if none := full.Filter(".*"); len(none.Procs)+len(none.Units) != 0 {
		t.Fatal("query must be literal, not a regular expression")
	}
}

func TestSubjectFilterSearchesOnlyKnownNamesAndOperands(t *testing.T) {
	tests := []struct {
		name string
		proc ProcRow
		unit UnitRow
		want bool
	}{
		{name: "command", proc: ProcRow{Process: Process{Kind: "needle"}}, unit: UnitRow{Job: Job{Kind: "needle"}}, want: true},
		{name: "process unit", proc: ProcRow{Process: Process{Unit: "needle.service"}}, want: true},
		{name: "service", proc: ProcRow{Unit: Unit{Name: "needle.service"}}, unit: UnitRow{Unit: Unit{Name: "needle.service"}}, want: true},
		{name: "timer", proc: ProcRow{Timer: Unit{Name: "needle.timer"}}, unit: UnitRow{Timer: Unit{Name: "needle.timer"}}, want: true},
		{name: "target", proc: ProcRow{Process: Process{Target: "/needle"}}, want: true},
		{name: "unit log", proc: ProcRow{Unit: Unit{LogFile: "/needle.log"}}, unit: UnitRow{Unit: Unit{LogFile: "/needle.log"}}, want: true},
		{name: "job log", proc: ProcRow{Job: Job{LogFile: "/needle.log"}}, unit: UnitRow{Job: Job{LogFile: "/needle.log"}}, want: true},
		{name: "first bisync operand", proc: ProcRow{Job: Job{Path1: "needle:"}}, unit: UnitRow{Job: Job{Path1: "needle:"}}, want: true},
		{name: "second bisync operand", proc: ProcRow{Job: Job{Path2: "/needle"}}, unit: UnitRow{Job: Job{Path2: "/needle"}}, want: true},
		{name: "arguments", proc: ProcRow{Process: Process{Args: []string{"needle"}}}},
		{name: "working directory", proc: ProcRow{Process: Process{Cwd: "/needle"}}},
		{name: "scope", proc: ProcRow{Unit: Unit{Scope: "needle"}}, unit: UnitRow{Unit: Unit{Scope: "needle"}}},
		{name: "read error", proc: ProcRow{Job: Job{ReadError: "needle"}}, unit: UnitRow{Job: Job{ReadError: "needle"}}},
		{name: "outcome", proc: ProcRow{Job: Job{Outcome: "needle"}}, unit: UnitRow{Job: Job{Outcome: "needle"}}},
		{name: "missing facts"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := View{Procs: []ProcRow{tt.proc}, Units: []UnitRow{tt.unit}}
			got := v.Filter("needle")
			if (len(got.Procs) == 1) != tt.want {
				t.Fatalf("process match = %v, want %v", len(got.Procs) == 1, tt.want)
			}
			// Process-only facts have no counterpart on a finished service.
			unitWant := tt.want && tt.name != "process unit" && tt.name != "target"
			if (len(got.Units) == 1) != unitWant {
				t.Fatalf("service match = %v, want %v", len(got.Units) == 1, unitWant)
			}
		})
	}
}

func TestSubjectFilterMatchesUnicodeCaseAndPreservesInternalSpaces(t *testing.T) {
	v := View{Procs: []ProcRow{{Process: Process{Paths: []string{"/Σπίτι/Backup  Casa"}}}}}
	if len(v.Filter("ςΠΊΤΙ").Procs) != 1 {
		t.Fatal("Unicode case variants must match")
	}
	if len(v.Filter("backup casa").Procs) != 0 {
		t.Fatal("internal spaces must remain literal")
	}
}

func TestSubjectFilterRetainsHostContextAndHealthWithoutOverwritingSubjects(t *testing.T) {
	s := NewState()
	s.Processes = []Process{{PID: 7, Kind: KindCopy, Paths: []string{"/keep"}}, {PID: 8, Kind: KindSync}}
	s.Units = []Unit{{Name: "keep.service"}, {Name: "other.service"}}
	s.Mounts = []Mount{{Mountpoint: "/orphan"}}
	s.Caches = []CacheDir{{}}
	s.SyncPairs = []SyncPair{{}}
	s.Seen[SourceProc] = time.Now()
	failure := errors.New("collector failed")
	s.Fail(SourceLog, failure)
	full := s.Resolve()
	got := full.Filter("keep")
	if len(got.Orphans) != 1 || got.Orphans[0].Mountpoint != "/orphan" || len(got.Caches) != 1 || len(got.Pairs) != 1 || got.Seen[SourceProc] != full.Seen[SourceProc] || got.Errors[SourceLog] != failure {
		t.Fatal("filter discarded host context or collector health")
	}
	got.Procs[0] = ProcRow{}
	got.Units[0] = UnitRow{}
	if full.Procs[0].Process.PID != 7 || full.Units[0].Unit.Name != "keep.service" {
		t.Fatal("filtered subject slices overwrite full view")
	}
	if len(s.Processes) != 2 || len(s.Units) != 2 {
		t.Fatal("filter changed collected facts")
	}
}
