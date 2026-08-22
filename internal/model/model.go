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

// Mount is an active rclone FUSE mount.
type Mount struct {
	Remote     string // the remote spec being served, e.g. "gdrive:"
	Mountpoint string
	FSType     string // "fuse.rclone" and its variants
	Source     Source
}

// CacheDir is a local directory rclone caches into. Its size is the part of a
// remote that is actually costing disk here, which is the question "how much
// space is this using" usually means on the local side.
type CacheDir struct {
	Kind      string // the subdirectory under rclone's cache root: vfs, vfsMeta, ...
	Path      string
	Bytes     uint64
	Files     int
	ScannedAt time.Time
}

// SyncSide is one end of a bisync pair, as recorded in its cached listing.
type SyncSide struct {
	Label string // the side's name as bisync mangles it into the filename
	Files int
	Bytes uint64
}

// SyncPair is a bisync session reconstructed from the listings rclone leaves in
// its cache directory.
//
// This is the cheapest real answer to "how many files are synchronised and how
// much space do they take": the listings are a complete census of both sides,
// already on local disk, so reading them costs no API calls and no network.
// The catch is that they describe the last run, not this instant.
type SyncPair struct {
	Name        string
	Left, Right SyncSide

	// Drift is the number of paths that differ between the two sides: present
	// on one only, or present on both at different sizes. Zero means the last
	// run left the two ends agreeing.
	Drift int

	// ListedAt is when bisync wrote the listing, taken from its header. It is
	// therefore the time of the last run that got far enough to list.
	ListedAt time.Time

	// FailedAt is set when a .lst-err file is present, which is how bisync
	// records that a run did not finish cleanly. It outlives the run, so a
	// stale one means "the last failure was then", not "it is failing now".
	FailedAt time.Time

	Source Source
}

// Snapshot is one observation of rclone activity by a single collector. A
// collector only fills the fields it knows about; the rest stay nil and are
// filled in by other collectors covering the same moment.
type Snapshot struct {
	At        time.Time
	Source    Source
	Processes []Process
	Mounts    []Mount
	Caches    []CacheDir
	SyncPairs []SyncPair
}

// State is the merged view the UI renders, assembled from the most recent
// snapshot of each collector.
type State struct {
	Processes []Process
	Mounts    []Mount
	Caches    []CacheDir
	SyncPairs []SyncPair

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
	// A nil slice means "this collector has nothing to say about that", an
	// empty one means "it looked and found none". Only the latter should clear
	// what is on screen.
	if snap.Processes != nil {
		s.Processes = snap.Processes
	}
	if snap.Mounts != nil {
		s.Mounts = snap.Mounts
	}
	if snap.Caches != nil {
		s.Caches = snap.Caches
	}
	if snap.SyncPairs != nil {
		s.SyncPairs = snap.SyncPairs
	}
	s.Seen[snap.Source] = snap.At
	delete(s.Errors, snap.Source)
}

// Fail records that a collector failed, without discarding the data it
// reported earlier. Stale data clearly marked as stale beats an empty screen.
func (s *State) Fail(src Source, err error) {
	s.Errors[src] = err
}
