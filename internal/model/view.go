package model

import (
	"sort"
	"time"
)

// View is the merged state, with every cross-source question already answered.
//
// State keeps what each collector said, separately, because that is how the
// facts arrive and because a collector owns the slices it fills. But almost
// nothing on screen is one collector's answer alone: a process line carries the
// progress its log reports and the journal errors its unit recorded, a unit line
// is suppressed entirely when a process line already describes it, and a mount
// with no process behind it is a finding that neither source can state on its
// own.
//
// Those joins are on natural keys -- PID, unit name, log file, mountpoint --
// and they used to live in the renderers, mixed in with the colour. Here they
// are plain data, which is the only form in which they can be tested or reused
// by a second view.
type View struct {
	Procs   []ProcRow
	Orphans []Mount
	Pairs   []SyncPair
	Units   []UnitRow
	Caches  []CacheDir

	// Seen and Errors are State's own maps, not copies. A View is built for one
	// frame and read, never written.
	Seen   map[Source]time.Time
	Errors map[Source]error
}

// ProcRow is a running process together with everything known about it from
// elsewhere.
type ProcRow struct {
	Process Process

	// RCStats is the exact accounting reported by the daemon this process
	// serves, when that daemon was discovered and answered.
	RCStats *RCStats

	// Job is what the log this process writes has to say about the run, and the
	// zero value when no log was found for it.
	//
	// The zero stands in for a (Job, bool) pair, and that only works because
	// every field a renderer reads gates on something first: ReadError on being
	// non-empty, the statistics on HaveStats, the outcome on Outcome. It is
	// load-bearing rather than incidental. A field added to Job that a renderer
	// would draw unconditionally has to arrive with its own "is this known"
	// companion, or a process with no log behind it renders a zero -- and a
	// zero and a missing measurement mean opposite things to someone checking
	// whether their backup ran.
	Job Job

	// Errors are the unit's journal entries followed by the job's own log
	// lines. The two are disjoint in practice rather than duplicated: a job
	// started with --log-file writes nothing to the journal, and one without it
	// has no log file to read.
	Errors []LogLine
}

// UnitRow is a service together with the timer that starts it and the log it
// writes.
type UnitRow struct {
	Unit Unit

	// Timer is the timer that activates this service, or the zero value when
	// none does. A timer that is present but has no NextElapse has been
	// stopped, which is a different thing from having no timer at all and must
	// stay tellable apart.
	Timer Unit

	// Job is what the file this unit names was read to contain, and the zero
	// value when it names none or none has been read. It carries the same
	// obligation as a ProcRow's.
	Job Job

	// LastRun is when the most recent run began or ended, resolved against the
	// timer's own record of when it last fired.
	LastRun time.Time

	// Errors are the unit's journal entries followed by its log's, on the same
	// grounds as a ProcRow's.
	Errors []LogLine
}

// Resolve merges the per-source state into the rows a view renders.
//
// It takes no clock: nothing here depends on the current time. Ageing a
// timestamp for display is the renderer's business, and keeping it out means
// the result is a pure function of the state, which is what makes it testable
// without a fixed epoch.
func (s *State) Resolve() View {
	shown := s.unitsShownAsProcesses()

	return View{
		Procs:   s.procRows(),
		Orphans: s.orphanMounts(),
		Pairs:   s.SyncPairs,
		Units:   s.unitRows(shown),
		Caches:  s.Caches,
		Seen:    s.Seen,
		Errors:  s.Errors,
	}
}

// procRows builds one row per running process, busiest first.
func (s *State) procRows() []ProcRow {
	rows := make([]ProcRow, 0, len(s.Processes))
	for _, p := range s.Processes {
		job := s.jobForPID(p.PID)
		rc := s.rcStatsForAddr(p.RCAddr)
		if rc != nil {
			job.Stats = mergeStats(job.Stats, rc.Stats, job.HaveStats)
			job.HaveStats = job.HaveStats || rc.Stats.Known != 0
		}
		rows = append(rows, ProcRow{
			Process: p,
			RCStats: rc,
			Job:     job,
			Errors:  concatLines(s.unitErrorsFor(p), job.Errors),
		})
	}

	// Long-lived services first, then the busiest: a mount that is always there
	// should not jump around as one-shot jobs come and go.
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := rows[i].Process.Kind.IsService(), rows[j].Process.Kind.IsService()
		if li != lj {
			return li
		}
		a, b := rows[i].Process, rows[j].Process
		return a.ReadRate+a.WriteRate > b.ReadRate+b.WriteRate
	})
	return rows
}

// mergeStats prefers RC measurements one field at a time. An endpoint can
// implement core/stats partially, and an absent field must leave the local log
// observation intact rather than turn it into a misleading zero.
func mergeStats(local, exact JobStats, localKnown bool) JobStats {
	merged := local
	if merged.Source == "" {
		merged.Source = SourceLog
	}
	if merged.Sources == nil {
		merged.Sources = make(map[StatsFields]Source)
	}
	if localKnown {
		for field := StatsBytes; field <= StatsETA; field <<= 1 {
			if merged.Known&field != 0 {
				merged.Sources[field] = merged.Source
			}
		}
	}
	for field := StatsBytes; field <= StatsETA; field <<= 1 {
		if exact.Known&field == 0 {
			continue
		}
		merged.Known |= field
		merged.Sources[field] = SourceRC
		if !localKnown {
			merged.Source = SourceRC
		}
		switch field {
		case StatsBytes:
			merged.Bytes = exact.Bytes
		case StatsTotalBytes:
			merged.TotalBytes = exact.TotalBytes
		case StatsTransfers:
			merged.Transfers = exact.Transfers
		case StatsTotalTransfers:
			merged.TotalTransfers = exact.TotalTransfers
		case StatsChecks:
			merged.Checks = exact.Checks
		case StatsTotalChecks:
			merged.TotalChecks = exact.TotalChecks
		case StatsErrors:
			merged.Errors = exact.Errors
		case StatsFatalError:
			merged.FatalError = exact.FatalError
		case StatsDeletes:
			merged.Deletes = exact.Deletes
		case StatsRenames:
			merged.Renames = exact.Renames
		case StatsSpeed:
			merged.Speed = exact.Speed
		case StatsElapsed:
			merged.Elapsed = exact.Elapsed
		case StatsETA:
			merged.ETA, merged.ETAKnown = exact.ETA, exact.ETAKnown
		}
	}
	return merged
}

func (s *State) rcStatsForAddr(addr string) *RCStats {
	if addr == "" {
		return nil
	}
	for i := range s.RCStats {
		if s.RCStats[i].Addr == addr {
			stats := s.RCStats[i]
			return &stats
		}
	}
	return nil
}

// unitRows builds one row per service worth a line of its own, failures first.
//
// Services and their timers are folded together. Presenting them separately
// would double the length of the section and split the two halves of a single
// answer: a timer's schedule is only meaningful next to the result of the job
// it starts.
func (s *State) unitRows(shown map[string]bool) []UnitRow {
	timers := make(map[string]Unit)
	var services []Unit
	for _, u := range s.Units {
		if u.IsTimer() {
			// Two timers can start the same service. Keep the one due first,
			// since that is the answer to "when does this next run"; picking
			// arbitrarily would make the display depend on map ordering.
			if u.Triggers == "" {
				continue
			}
			if prev, ok := timers[u.Triggers]; ok && sooner(prev.NextElapse, u.NextElapse) {
				continue
			}
			timers[u.Triggers] = u
			continue
		}
		// A unit whose process is already on screen would otherwise be
		// described twice, and the two descriptions would say the same thing in
		// different words: "up 14h40m" against "running for 14h40m". The
		// process line wins, because it carries the throughput; what only the
		// unit knows -- its journal errors -- goes there instead.
		if shown[u.Name] {
			continue
		}
		services = append(services, u)
	}

	// Timers whose service was not itself reported still deserve a line.
	for target, t := range timers {
		if !shown[target] && !hasUnit(services, target) {
			services = append(services, Unit{Name: target, Scope: t.Scope, Source: t.Source})
		}
	}

	// Failures first, then by name, so a problem never scrolls out of reach
	// behind healthy units. Sorting by name also settles the order of the
	// synthesised rows above, which were appended in map order.
	sort.SliceStable(services, func(i, j int) bool {
		fi, fj := services[i].Failed(), services[j].Failed()
		if fi != fj {
			return fi
		}
		return services[i].Name < services[j].Name
	})

	rows := make([]UnitRow, 0, len(services))
	for _, u := range services {
		timer := timers[u.Name]
		job := s.jobForLogFile(u.LogFile)
		rows = append(rows, UnitRow{
			Unit:    u,
			Timer:   timer,
			Job:     job,
			LastRun: u.LastRun(timer.LastTrigger),
			Errors:  concatLines(u.Errors, job.Errors),
		})
	}
	return rows
}

// unitsShownAsProcesses names the units already represented by a process.
func (s *State) unitsShownAsProcesses() map[string]bool {
	shown := make(map[string]bool)
	for _, p := range s.Processes {
		if p.Unit != "" {
			shown[p.Unit] = true
		}
	}
	return shown
}

// orphanMounts are FUSE mounts with no live rclone process behind them. That
// combination is a real failure mode -- the process died and left the
// mountpoint wedged -- and it is invisible to either source alone.
func (s *State) orphanMounts() []Mount {
	if len(s.Mounts) == 0 {
		return nil
	}
	served := make(map[string]bool, len(s.Processes))
	for _, p := range s.Processes {
		if p.Kind == KindMount {
			served[p.Target] = true
		}
	}

	var orphans []Mount
	for _, m := range s.Mounts {
		if !served[m.Mountpoint] {
			orphans = append(orphans, m)
		}
	}
	return orphans
}

// jobForPID returns what the log a process writes says about its run.
//
// The match is exact: the log file was discovered from that process's own
// command line. A job whose process has exited keeps its last statistics but no
// longer matches anything, and so quietly stops being drawn against a process.
func (s *State) jobForPID(pid int) Job {
	for _, j := range s.Jobs {
		if j.PID != 0 && j.PID == pid {
			return j
		}
	}
	return Job{}
}

// jobForLogFile returns what the log collector read from the file a unit names.
// This is the only account of a scheduled job between its runs: one started
// with --log-file writes nothing to the journal.
func (s *State) jobForLogFile(path string) Job {
	if path == "" {
		return Job{}
	}
	for _, j := range s.Jobs {
		if j.LogFile == path {
			return j
		}
	}
	return Job{}
}

// unitErrorsFor returns the journal errors of the unit that owns a process, so
// they can be shown against the process rather than lost with its unit line.
func (s *State) unitErrorsFor(p Process) []LogLine {
	if p.Unit == "" {
		return nil
	}
	for _, u := range s.Units {
		if u.Name == p.Unit {
			return u.Errors
		}
	}
	return nil
}

// concatLines joins two sets of log lines into a slice of its own, so a row
// never aliases a slice a collector goes on appending to.
func concatLines(a, b []LogLine) []LogLine {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]LogLine, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

func hasUnit(units []Unit, name string) bool {
	for _, u := range units {
		if u.Name == name {
			return true
		}
	}
	return false
}

// sooner reports whether a comes before b, treating "never" as last.
func sooner(a, b time.Time) bool {
	switch {
	case a.IsZero():
		return false
	case b.IsZero():
		return true
	default:
		return a.Before(b)
	}
}
