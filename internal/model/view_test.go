package model

import (
	"context"
	"testing"
	"time"
)

// stateWith builds a State the way Apply would have left it, without going
// through a collector.
func stateWith(s State) *State {
	s.Seen = map[Source]time.Time{}
	s.Errors = map[Source]error{}
	return &s
}

func TestRCStatsAreMatchedToTheirProcessByAddress(t *testing.T) {
	s := stateWith(State{
		Processes: []Process{
			{PID: 42, RCAddr: "127.0.0.1:5572"},
			{PID: 43, RCAddr: "127.0.0.1:5573"},
		},
		RCStats: []RCStats{{
			Addr:  "127.0.0.1:5572",
			Stats: JobStats{Bytes: 1234, TotalBytes: 5678},
		}},
	})

	rows := s.Resolve().Procs
	if rows[0].RCStats == nil || rows[0].RCStats.Stats.Bytes != 1234 {
		t.Fatalf("first process did not receive its RC statistics: %+v", rows[0].RCStats)
	}
	if rows[1].RCStats != nil {
		t.Fatalf("unmatched endpoint received statistics: %+v", rows[1].RCStats)
	}
}

func TestResolvePrefersRCMeasurementsAndKeepsLocalFallbacks(t *testing.T) {
	local := JobStats{Bytes: 10, TotalBytes: 20, Checks: 3, TotalChecks: 4, Speed: 5}
	exact := JobStats{Bytes: 100, TotalBytes: 200, Speed: 50, Known: StatsBytes | StatsTotalBytes | StatsSpeed, Source: SourceRC}
	s := stateWith(State{
		Processes: []Process{{PID: 42, RCAddr: "rc:1"}},
		Jobs:      []Job{{PID: 42, HaveStats: true, Stats: local}},
		RCStats:   []RCStats{{Addr: "rc:1", Stats: exact}},
	})

	stats := s.Resolve().Procs[0].Job.Stats
	if stats.Bytes != 100 || stats.TotalBytes != 200 || stats.Speed != 50 {
		t.Fatalf("RC values did not win: %+v", stats)
	}
	if stats.Checks != 3 || stats.TotalChecks != 4 {
		t.Fatalf("local value was not retained for an absent RC field: %+v", stats)
	}
	if stats.Source != SourceLog || stats.Sources[StatsBytes] != SourceRC || stats.Sources[StatsChecks] != SourceLog {
		t.Errorf("sources = %q/%v, want mixed RC and log provenance", stats.Source, stats.Sources)
	}
}

func TestResolveUsesRCZeroAsAMeasurement(t *testing.T) {
	s := stateWith(State{
		Processes: []Process{{PID: 42, RCAddr: "rc:1"}},
		Jobs:      []Job{{PID: 42, HaveStats: true, Stats: JobStats{Bytes: 9, TotalBytes: 10}}},
		RCStats: []RCStats{{Addr: "rc:1", Stats: JobStats{
			Known: StatsBytes, Source: SourceRC,
		}}},
	})
	if got := s.Resolve().Procs[0].Job.Stats.Bytes; got != 0 {
		t.Fatalf("RC zero was treated as absent: got %d", got)
	}
}

func TestRCFailureDoesNotEraseLastValidLocalMeasurement(t *testing.T) {
	s := NewState()
	s.Apply(Snapshot{Source: SourceLog, At: time.Unix(1, 0), Jobs: []Job{{
		PID: 42, HaveStats: true, Stats: JobStats{Bytes: 7, TotalBytes: 9},
	}}})
	s.Fail(SourceRC, context.Canceled)
	s.Apply(Snapshot{Source: SourceProc, At: time.Unix(2, 0), Processes: []Process{{PID: 42, RCAddr: "rc:1"}}})

	row := s.Resolve().Procs[0]
	if row.Job.Stats.Bytes != 7 || row.Job.Stats.TotalBytes != 9 {
		t.Fatalf("local measurement was lost after RC failure: %+v", row.Job.Stats)
	}
	if row.RCStats != nil {
		t.Fatalf("failed RC source produced statistics: %+v", row.RCStats)
	}
}

// A job is matched to a process by PID, and only by PID. The log file was
// discovered from that process's own command line, so the match is exact --
// but a job whose process has exited keeps its statistics and reports PID 0,
// and a zero must never match the zero of an unset field.
func TestAJobIsMatchedToItsProcessByPID(t *testing.T) {
	s := stateWith(State{
		Processes: []Process{{PID: 42}},
		Jobs: []Job{
			{LogFile: "/var/log/other.log", PID: 99, Outcome: "successful"},
			{LogFile: "/var/log/finished.log", PID: 0, Outcome: "aborted"},
			{LogFile: "/var/log/mine.log", PID: 42, HaveStats: true},
		},
	})

	rows := s.Resolve().Procs
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want one", len(rows))
	}
	if rows[0].Job.LogFile != "/var/log/mine.log" {
		t.Errorf("got %q, want the log this process writes", rows[0].Job.LogFile)
	}

	// And a process no log describes carries the zero Job, which every renderer
	// reads as "nothing to say".
	bare := stateWith(State{Processes: []Process{{PID: 7}}})
	if job := bare.Resolve().Procs[0].Job; job.LogFile != "" || job.HaveStats {
		t.Errorf("got %+v, want the zero job", job)
	}

	// The zero of an unread PID must not match the zero a finished job reports.
	// /proc can fail to give a PID, and adopting the record of a run that ended
	// hours ago would describe this process with somebody else's statistics.
	unread := stateWith(State{
		Processes: []Process{{PID: 0}},
		Jobs:      []Job{{LogFile: "/var/log/finished.log", PID: 0, Outcome: "aborted"}},
	})
	if job := unread.Resolve().Procs[0].Job; job.LogFile != "" {
		t.Errorf("a process with no PID adopted %q", job.LogFile)
	}
}

// A unit whose process is on screen is described by the process line, which
// carries the throughput as well. The unit's own line would say the same thing
// in different words -- but its journal errors are known to nobody else, so
// they move to the process rather than disappearing with the line.
func TestAUnitShownAsAProcessMovesItsErrorsThere(t *testing.T) {
	errs := []LogLine{{Message: "quota exceeded", Priority: 3}}
	s := stateWith(State{
		Processes: []Process{{PID: 42, Unit: "rclone-mount.service"}},
		Units: []Unit{
			{Name: "rclone-mount.service", Errors: errs},
			{Name: "unrelated.service"},
		},
	})

	v := s.Resolve()
	if len(v.Units) != 1 || v.Units[0].Unit.Name != "unrelated.service" {
		t.Errorf("got %v, want only the unit with no process of its own", v.Units)
	}
	if len(v.Procs[0].Errors) != 1 || v.Procs[0].Errors[0].Message != "quota exceeded" {
		t.Errorf("the unit's journal errors did not move to its process: %v", v.Procs[0].Errors)
	}
}

// The journal's errors and the log's are concatenated in that order, and into a
// slice of the row's own: the collectors go on appending to theirs between
// ticks, and a row handed out earlier must not change underneath the screen.
func TestErrorsAreJoinedIntoTheRowsOwnSlice(t *testing.T) {
	unitErrs := []LogLine{{Message: "from the journal"}}
	s := stateWith(State{
		Processes: []Process{{PID: 42, Unit: "job.service"}},
		Units:     []Unit{{Name: "job.service", Errors: unitErrs}},
		Jobs:      []Job{{PID: 42, Errors: []LogLine{{Message: "from the log"}}}},
	})

	got := s.Resolve().Procs[0].Errors
	if len(got) != 2 || got[0].Message != "from the journal" || got[1].Message != "from the log" {
		t.Fatalf("got %v, want the journal's followed by the log's", got)
	}

	// With nothing to join it to, the journal's slice is still copied rather
	// than handed straight out. This is the case that bites: the collector
	// retains that slice between ticks and appends to it, and both it and the
	// log collector already copy on the way out for the same reason.
	alone := stateWith(State{
		Processes: []Process{{PID: 42, Unit: "job.service"}},
		Units:     []Unit{{Name: "job.service", Errors: unitErrs}},
	})
	row := alone.Resolve().Procs[0].Errors
	if len(row) != 1 {
		t.Fatalf("got %v, want the journal's line", row)
	}
	row[0].Message = "rewritten"
	if unitErrs[0].Message != "from the journal" {
		t.Error("the row aliases the slice the collector owns")
	}
}

// A mount with no rclone process behind it is a real failure mode -- the
// process died and left the mountpoint wedged -- and neither source can state
// it alone.
func TestOrphanMountsAreTheOnesNoProcessServes(t *testing.T) {
	s := stateWith(State{
		Processes: []Process{{PID: 1, Kind: KindMount, Target: "/mnt/live"}},
		Mounts: []Mount{
			{Mountpoint: "/mnt/live"},
			{Mountpoint: "/mnt/wedged"},
		},
	})

	orphans := s.Resolve().Orphans
	if len(orphans) != 1 || orphans[0].Mountpoint != "/mnt/wedged" {
		t.Errorf("got %v, want only the wedged mountpoint", orphans)
	}
}

// Long-lived services first, then the busiest. A mount that is always there
// should not jump around as one-shot jobs come and go.
func TestServicesSortAheadOfTheBusiest(t *testing.T) {
	s := stateWith(State{Processes: []Process{
		{PID: 1, Kind: KindSync, ReadRate: 10, WriteRate: 10},
		{PID: 2, Kind: KindMount, ReadRate: 1},
		{PID: 3, Kind: KindCopy, ReadRate: 50},
	}})

	var order []int
	for _, row := range s.Resolve().Procs {
		order = append(order, row.Process.PID)
	}
	if len(order) != 3 || order[0] != 2 || order[1] != 3 || order[2] != 1 {
		t.Errorf("got %v, want the mount first and then by rate", order)
	}
}

// Failures first, so a problem never scrolls out of reach behind healthy units.
// Below that, by name -- which also settles the order of the rows synthesised
// out of the timer map, whose iteration order is not defined.
func TestFailedUnitsSortFirst(t *testing.T) {
	s := stateWith(State{Units: []Unit{
		{Name: "a.service"},
		{Name: "z.service", Result: "exit-code"},
		{Name: "b.service"},
	}})

	var order []string
	for _, row := range s.Resolve().Units {
		order = append(order, row.Unit.Name)
	}
	if len(order) != 3 || order[0] != "z.service" || order[1] != "a.service" {
		t.Errorf("got %v, want the failure first", order)
	}
}

// Two timers can start the same service. The one due first is the answer to
// "when does this next run"; picking either would make the display depend on
// map ordering.
func TestTheSoonerOfTwoTimersWins(t *testing.T) {
	now := time.Unix(1787433722, 0)
	hourly := Unit{Name: "hourly.timer", Triggers: "backup.service", NextElapse: now.Add(time.Hour)}
	nightly := Unit{Name: "nightly.timer", Triggers: "backup.service", NextElapse: now.Add(8 * time.Hour)}

	// Both orders, because the whole point is that the answer does not depend
	// on which of the two was listed first.
	for _, order := range [][]Unit{{hourly, nightly}, {nightly, hourly}} {
		s := stateWith(State{Units: append([]Unit{{Name: "backup.service"}}, order...)})

		rows := s.Resolve().Units
		if len(rows) != 1 {
			t.Fatalf("got %d rows, want the service alone", len(rows))
		}
		if rows[0].Timer.Name != "hourly.timer" {
			t.Errorf("listed %q first, got %q, want the timer due first",
				order[0].Name, rows[0].Timer.Name)
		}
	}

	// A timer that has been stopped has no next elapse at all, and "never" is
	// not sooner than tomorrow.
	stopped := Unit{Name: "stopped.timer", Triggers: "backup.service"}
	for _, order := range [][]Unit{{stopped, nightly}, {nightly, stopped}} {
		s := stateWith(State{Units: append([]Unit{{Name: "backup.service"}}, order...)})
		if got := s.Resolve().Units[0].Timer.Name; got != "nightly.timer" {
			t.Errorf("listed %q first, got %q, want the one that will actually fire",
				order[0].Name, got)
		}
	}
}

// A timer whose service was not itself reported still deserves a line: the
// schedule is the answer even when there is nothing else to say.
func TestATimerWithoutItsServiceStillGetsARow(t *testing.T) {
	s := stateWith(State{Units: []Unit{
		{Name: "backup.timer", Scope: "user", Triggers: "backup.service",
			NextElapse: time.Unix(1787433722, 0)},
	}})

	rows := s.Resolve().Units
	if len(rows) != 1 || rows[0].Unit.Name != "backup.service" {
		t.Fatalf("got %v, want a row for the service the timer starts", rows)
	}
	if rows[0].Unit.Scope != "user" {
		t.Errorf("the synthesised row lost the timer's scope: %q", rows[0].Unit.Scope)
	}
	// But not when a process is already showing it: that would be the third
	// description of one job.
	withProc := stateWith(State{
		Processes: []Process{{PID: 1, Unit: "backup.service"}},
		Units:     []Unit{{Name: "backup.timer", Triggers: "backup.service"}},
	})
	if rows := withProc.Resolve().Units; len(rows) != 0 {
		t.Errorf("got %v, want nothing for a service already on screen", rows)
	}
}

// Between runs the unit's log is the only account of how the last one went, and
// it is found by the path the unit names -- the only key the two sources share.
func TestAUnitFindsItsJobByLogFile(t *testing.T) {
	s := stateWith(State{
		Units: []Unit{{Name: "backup.service", LogFile: "/var/log/backup.log"}},
		Jobs: []Job{
			{LogFile: "/var/log/other.log", Outcome: "aborted"},
			{LogFile: "/var/log/backup.log", Outcome: "successful"},
		},
	})

	if got := s.Resolve().Units[0].Job.Outcome; got != "successful" {
		t.Errorf("got %q, want the outcome from the file this unit names", got)
	}

	// A unit that names no log file matches nothing, rather than matching the
	// first job whose LogFile is also empty.
	none := stateWith(State{
		Units: []Unit{{Name: "backup.service"}},
		Jobs:  []Job{{LogFile: "", Outcome: "aborted"}},
	})
	if got := none.Resolve().Units[0].Job.Outcome; got != "" {
		t.Errorf("got %q from a unit that names no log", got)
	}
}

// LastRun is state-dependent, and the timer's own record of when it last fired
// is the fallback for a unit systemd has nothing to say about -- which is a
// service that has never run since boot.
func TestLastRunFallsBackToTheTimersTrigger(t *testing.T) {
	fired := time.Unix(1787433722, 0)
	s := stateWith(State{Units: []Unit{
		{Name: "backup.service"},
		{Name: "backup.timer", Triggers: "backup.service", LastTrigger: fired},
	}})

	if got := s.Resolve().Units[0].LastRun; !got.Equal(fired) {
		t.Errorf("got %v, want the timer's last trigger", got)
	}
}
