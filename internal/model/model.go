// Package model holds the unified view of rclone activity that every collector
// feeds into.
//
// Collectors observe rclone from very different angles -- the rc API, systemd,
// log files, /proc -- and frequently describe the same object. The UI must not
// care which collector produced a number, only how trustworthy it is, which is
// why every value carries its Source.
package model

import "time"

// Source identifies the collector a piece of data came from. It travels with
// the data so the UI can mark a value as inferred rather than measured: a byte
// count from the rc API is exact, the same count derived from /proc is an
// approximation of the same thing.
type Source string

const (
	SourceProc    Source = "proc"
	SourceRC      Source = "rc"
	SourceSystemd Source = "systemd"
	SourceLog     Source = "log"
	SourceBisync  Source = "bisync"
	SourceLocalFS Source = "localfs"
)

// Kind is the rclone subcommand a process is running, derived from its command
// line. A mount and a one-shot sync need to be presented very differently: the
// former is a long-lived service, the latter has a beginning and an end.
type Kind string

const (
	KindMount   Kind = "mount"
	KindSync    Kind = "sync"
	KindBisync  Kind = "bisync"
	KindCopy    Kind = "copy"
	KindMove    Kind = "move"
	KindCheck   Kind = "check"
	KindServe   Kind = "serve"
	KindRCD     Kind = "rcd"
	KindUnknown Kind = "unknown"
)

// Process is a running rclone process discovered on the host.
type Process struct {
	PID     int
	Kind    Kind
	Source  Source
	Remotes []string // remote specs from the command line, e.g. "gdrive:"
	Target  string   // mountpoint for a mount, destination otherwise

	// Paths are the positional arguments in order: source then destination
	// for a transfer, remote then mountpoint for a mount. A two-sided job is
	// only legible with both ends on screen -- "gdrive:Documents" alone does
	// not say what is being synchronised with it.
	Paths []string

	Args      []string
	StartedAt time.Time
	RSS       uint64 // resident set size, bytes
	Threads   int

	// ReadTotal and WriteTotal are the cumulative counters from
	// /proc/<pid>/io. They only ever grow, so they must never be rendered as
	// a rate.
	ReadTotal  uint64
	WriteTotal uint64

	// ReadRate and WriteRate are bytes per second, derived from the delta
	// between two consecutive samples. They are zero on the first sample,
	// when there is nothing to compare against yet.
	ReadRate  float64
	WriteRate float64

	// IOAvailable is false when /proc/<pid>/io could not be read, which
	// happens for processes owned by another user. Without it the rates are
	// meaningless and the UI shows a placeholder rather than a fake zero.
	IOAvailable bool

	// RCAddr is the address this process serves the rc API on, if it was
	// started with --rc-addr. It is how the rc collector finds daemons to
	// talk to without ever scanning the network.
	RCAddr string
}

// Uptime reports how long the process has been running. It returns zero when
// the start time could not be determined.
func (p Process) Uptime() time.Duration {
	if p.StartedAt.IsZero() {
		return 0
	}
	return time.Since(p.StartedAt)
}

// Snapshot is one observation of rclone activity by a single collector. A
// collector only fills the fields it knows about; the rest stay zero and are
// filled in by other collectors covering the same moment.
type Snapshot struct {
	At        time.Time
	Source    Source
	Processes []Process
}

// State is the merged view the UI renders, assembled from the most recent
// snapshot of each collector.
type State struct {
	Processes []Process

	// Seen records the last time each collector reported successfully, so
	// the UI can tell "nothing is happening" apart from "this source went
	// quiet".
	Seen map[Source]time.Time

	// Errors holds the last error per collector, cleared on the next
	// successful collection.
	Errors map[Source]error
}

// NewState returns a State with its maps ready to use.
func NewState() *State {
	return &State{
		Seen:   make(map[Source]time.Time),
		Errors: make(map[Source]error),
	}
}

// Apply folds a collector's snapshot into the merged state.
//
// Merging is per-source rather than per-field for now: each collector owns the
// slices it fills. Cross-source merging on natural keys (PID, unit name, the
// (srcFs,dstFs) pair) arrives with the collectors that can actually describe
// the same job twice.
func (s *State) Apply(snap Snapshot) {
	if snap.Processes != nil {
		s.Processes = snap.Processes
	}
	s.Seen[snap.Source] = snap.At
	delete(s.Errors, snap.Source)
}

// Fail records that a collector failed, without discarding the data it
// reported earlier. Stale data clearly marked as stale beats an empty screen.
func (s *State) Fail(src Source, err error) {
	s.Errors[src] = err
}
