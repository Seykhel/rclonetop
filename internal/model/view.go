package model

import (
	"maps"
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

// SubjectID identifies a service across runs, or one lifetime of a manual process.
// An empty ID denotes no subject. Start time protects process IDs against reuse.
type SubjectID struct {
	Scope     string
	Unit      string
	PID       int
	StartedAt time.Time
}

func serviceID(u Unit) SubjectID { return SubjectID{Scope: u.Scope, Unit: u.Name} }

// ProcRow is a running process together with everything known about it from
// elsewhere.
type ProcRow struct {
	// ProcessSubject remains stable when newly collected ownership changes Subject.
	ProcessSubject SubjectID
	Subject        SubjectID
	Unit           Unit
	Timer          Unit
	LastRun        time.Time
	Process        Process

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

	// RecoveredAt is the owning unit's Unit.RecoveredAt, carried over here
	// because the unit itself gets no line of its own once its process is on
	// screen -- see unitRows. The zero value means what it means there: no
	// evidence yet that whatever Errors reports has been left behind.
	RecoveredAt time.Time
}

// UnitRow is a service together with the timer that starts it and the log it
// writes.
type UnitRow struct {
	Subject SubjectID
	Unit    Unit

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
			// Same "RC wins when it has an answer" rule as mergeStats, kept
			// separate because Transferring is not one of JobStats' fields:
			// a nil rc.Transferring means core/stats has not been polled at
			// all, not that it counted zero files, and must not overwrite a
			// log observation that already has.
			if rc.Transferring != nil {
				job.Transferring = rc.Transferring
			}
		}
		owner, owned := s.unitFor(p)
		subject := SubjectID{PID: p.PID, StartedAt: p.StartedAt}
		var timer Unit
		if owned {
			count := 0
			for _, other := range s.Processes {
				if unit, ok := s.unitFor(other); ok && serviceID(unit) == serviceID(owner) {
					count++
				}
			}
			if count == 1 {
				subject = serviceID(owner)
			}
			timer = s.timers()[serviceID(owner)]
		}
		rows = append(rows, ProcRow{
			Process:        p,
			ProcessSubject: SubjectID{PID: p.PID, StartedAt: p.StartedAt},
			Subject:        subject, Unit: owner, Timer: timer, LastRun: owner.LastRun(timer.LastTrigger),
			RCStats:     rc,
			Job:         job,
			Errors:      concatLines(owner.Errors, job.Errors),
			RecoveredAt: owner.RecoveredAt(),
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
	// The struct copy still shares the map with State.Jobs. Merging must not
	// rewrite the local sources a later frame needs when RC is absent.
	merged.Sources = maps.Clone(local.Sources)
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
func (s *State) unitRows(shown map[SubjectID]bool) []UnitRow {
	timers := s.timers()
	var services []Unit
	for _, u := range s.Units {
		if !u.IsTimer() && !shown[serviceID(u)] {
			services = append(services, u)
		}
	}

	// Timers whose service was not itself reported still deserve a line.
	for target, t := range timers {
		if !shown[target] && !hasUnit(services, target) {
			services = append(services, Unit{Name: target.Unit, Scope: t.Scope, Source: t.Source})
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
		timer := timers[serviceID(u)]
		job := s.jobForLogFile(u.LogFile)
		rows = append(rows, UnitRow{
			Subject: serviceID(u),
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
func (s *State) unitsShownAsProcesses() map[SubjectID]bool {
	shown := make(map[SubjectID]bool)
	for _, p := range s.Processes {
		if owner, ok := s.unitFor(p); ok {
			shown[serviceID(owner)] = true
		}
	}
	return shown
}

func (s *State) timers() map[SubjectID]Unit {
	timers := make(map[SubjectID]Unit)
	for _, u := range s.Units {
		if !u.IsTimer() || u.Triggers == "" {
			continue
		}
		key := SubjectID{Scope: u.Scope, Unit: u.Triggers}
		if prev, ok := timers[key]; ok && (sooner(prev.NextElapse, u.NextElapse) || (prev.NextElapse.Equal(u.NextElapse) && prev.Name < u.Name)) {
			continue
		}
		timers[key] = u
	}
	return timers
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

// unitFor returns the unit that owns a process, if any is recorded.
func (s *State) unitFor(p Process) (Unit, bool) {
	if p.Unit == "" {
		return Unit{}, false
	}
	var owner Unit
	found := false
	if p.UnitScope == "" && p.PID != 0 {
		for _, u := range s.Units {
			if u.Name != p.Unit || u.IsTimer() || u.MainPID != p.PID {
				continue
			}
			if found && owner.Scope != u.Scope {
				return Unit{}, false
			}
			owner, found = u, true
		}
		if found {
			return owner, true
		}
	}
	for _, u := range s.Units {
		if u.IsTimer() || u.Name != p.Unit || (p.UnitScope != "" && p.UnitScope != u.Scope) {
			continue
		}
		if found && owner.Scope != u.Scope {
			return Unit{}, false
		}
		owner, found = u, true
	}
	// A timer alone can establish the same scoped service representation.
	if !found {
		for key, timer := range s.timers() {
			if key.Unit != p.Unit || (p.UnitScope != "" && key.Scope != p.UnitScope) {
				continue
			}
			if found && owner.Scope != key.Scope {
				return Unit{}, false
			}
			owner, found = Unit{Name: key.Unit, Scope: key.Scope, Source: timer.Source}, true
		}
	}
	return owner, found
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

func hasUnit(units []Unit, id SubjectID) bool {
	for _, u := range units {
		if serviceID(u) == id {
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
