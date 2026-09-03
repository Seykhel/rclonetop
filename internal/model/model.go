// Package model holds the unified view of rclone activity that every collector
// feeds into.
//
// Collectors observe rclone from very different angles -- the rc API, systemd,
// log files, /proc -- and frequently describe the same object. The UI must not
// care which collector produced a number, only how trustworthy it is, which is
// why every value carries its Source.
package model

import (
	"strconv"
	"strings"
	"time"
)

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

// IsService reports whether this kind of job is a long-lived service rather
// than a run with a beginning and an end. The distinction decides where it sits
// on screen: a mount that is always there should not be reordered by one-shot
// jobs coming and going.
func (k Kind) IsService() bool {
	return k == KindMount || k == KindServe || k == KindRCD
}

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

	// Unit is the systemd unit that owns this process, read from its cgroup.
	// It is the only signal that attributes a unit whose ExecStart names a
	// wrapper script: whatever the unit file says, the rclone it spawns lands
	// in the unit's own cgroup.
	Unit string

	// RCAddr is the address this process serves the rc API on, if it was
	// started with --rc-addr. It is how the rc collector finds daemons to
	// talk to without ever scanning the network.
	RCAddr string

	// Cwd is the process's working directory, which is what a relative path on
	// its command line is relative to. Without it "--log-file rclone.log"
	// cannot be resolved to a file, and resolving it against rclonetop's own
	// directory would open a different file of the same name.
	Cwd string
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

	// Path is the operand as it was actually written, which only the log
	// collector can supply. It is empty for a session no log has described,
	// and the Label above is then all there is to show.
	Path string

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

// LogLine is one journal entry worth showing.
type LogLine struct {
	At       time.Time
	Priority int // syslog severity: 3 is err, 4 warning
	Message  string
}

// JobStats is rclone's own accounting for a run: how much of the work it set
// itself is done.
//
// Nothing else here can produce these numbers. /proc measures bytes moved
// through the kernel, which includes retries and metadata and knows nothing of
// how many files are still to come; only rclone knows what it set out to do.
type JobStats struct {
	// Known identifies measurements an RC response actually supplied. A zero
	// value means the legacy log representation, whose parsed fields retain the
	// existing best-effort semantics; RC uses the mask to distinguish an absent
	// field from a measured zero.
	Known  StatsFields
	Source Source

	Bytes      uint64
	TotalBytes uint64

	Transfers      int
	TotalTransfers int

	Checks      int
	TotalChecks int

	Errors     int
	FatalError bool

	Deletes int
	Renames int

	// Speed is rclone's own average over the run, which is a different
	// quantity from the instantaneous rate /proc gives: it is the figure that
	// answers "how long will this take", and it is what the ETA is derived
	// from.
	Speed   float64
	Elapsed time.Duration

	// ETA is only meaningful when ETAKnown is set. rclone writes "-" whenever
	// it cannot estimate -- which is most of a bisync, and all of any run whose
	// total is not yet known -- and a zero there would read as "done".
	ETA      time.Duration
	ETAKnown bool
}

// StatsFields identifies independently optional groups in rclone statistics.
type StatsFields uint16

const (
	StatsBytes StatsFields = 1 << iota
	StatsTotalBytes
	StatsTransfers
	StatsTotalTransfers
	StatsChecks
	StatsTotalChecks
	StatsErrors
	StatsFatalError
	StatsSpeed
	StatsElapsed
	StatsETA
)

// RCStats is rclone's exact accounting for the work handled by one rc daemon.
// The address is the only identity available without probing the daemon for a
// process id, and keeping it here lets Resolve associate the answer safely.
type RCStats struct {
	Addr   string
	Stats  JobStats
	Jobs   []RCJob
	At     time.Time
	Source Source
}

// RCJob is the lifecycle record rclone exposes for an asynchronous rc job.
// SuccessKnown matters because a missing success field is not evidence of a
// failure: older daemons and malformed responses must not be rendered as one.
type RCJob struct {
	ID           int
	Group        string
	Finished     bool
	Success      bool
	SuccessKnown bool
	Error        string
	Duration     time.Duration
	StartTime    time.Time
	EndTime      time.Time
}

// Done reports the fraction of the run's bytes that have moved, and whether
// that fraction means anything. A total of zero is a run with nothing to
// transfer, not a run that is nought per cent complete.
func (s JobStats) Done() (float64, bool) {
	if s.TotalBytes == 0 {
		return 0, false
	}
	return float64(s.Bytes) / float64(s.TotalBytes), true
}

// Transfer is one file rclone has in flight: what it is, how far through it is,
// and how fast it is going.
//
// Nothing but the log can produce this. /proc measures the whole process and
// knows nothing of files, systemd only has the result, and the bisync listings
// describe the last run rather than this instant -- so a screen that says *what*
// is moving, rather than only how much, has this and nothing else.
type Transfer struct {
	// Name is the file's path within the transfer, as rclone names it.
	//
	// From a text log it may have had its middle replaced by an ellipsis:
	// rclone shortens the name to fit its own column before writing it, and
	// that cannot be undone. It is one more reason to prefer a JSON log, where
	// the name arrives whole.
	Name string

	// Percentage is how far through this file rclone says it is, 0 to 100. It
	// is the one progress figure both log formats carry, and the only one the
	// text form carries at all.
	Percentage int

	// Bytes is how much of the file has moved, and BytesKnown is false for a
	// text log. That form prints the percentage and the total and leaves the
	// third figure out; deriving it from a percentage rounded to a whole number
	// would invent a measurement rclone never made.
	Bytes      uint64
	BytesKnown bool

	// Size is the file's total size, or -1 when rclone does not know it -- what
	// it records for a source that cannot be sized before the transfer starts.
	// Zero is a real answer, an empty file, so it cannot stand for this one.
	Size int64

	// Speed is this file's own rate in bytes per second, which is a different
	// quantity from JobStats.Speed: that one averages the whole run.
	//
	// The two formats do not measure it the same way. The JSON object's "speed"
	// is the file's average over its own life; the text line prints rclone's
	// moving average, the "speedAvg" beside it in the object, which reads zero
	// for the first seconds of every transfer. Where both are present the
	// average is taken, on the same grounds as everywhere else here: a zero and
	// "too early to say" mean opposite things.
	Speed float64

	// ETA is only meaningful when ETAKnown is set, for the same reason as
	// JobStats.ETA: rclone writes "-" in the text and null in the object
	// whenever it cannot estimate, and a zero there would read as "done".
	ETA      time.Duration
	ETAKnown bool
}

// Job is one rclone run as described by the log it is writing.
//
// The log is the only source that reports progress against a known total while
// the run is in flight. It is also the only one that writes a bisync pair's
// paths out in full: the listing filenames mangle them irreversibly.
type Job struct {
	// LogFile is the path being tailed, and the identity of the job. One log
	// file is one run at a time, whatever else changes.
	LogFile string

	// PID is the process writing the log, when one is still running. It is
	// zero for a job whose process has exited, which is how a live run is told
	// from the record of a finished one.
	PID  int
	Kind Kind

	// Path1 and Path2 are a bisync pair's operands as the log writes them,
	// which is to say in full and unmangled.
	Path1, Path2 string

	Stats     JobStats
	HaveStats bool

	// Transferring are the files in flight when the last statistics block was
	// written. Nil means nothing has said -- no block has been read yet, or the
	// run was started with --stats-one-line, which prints no file list at all --
	// and empty means a block was read and named none, which is what a run
	// between two files honestly looks like. The distinction is the same one
	// State.Apply keeps for a collector's slices, and for the same reason: only
	// the second should clear what is on screen.
	Transferring []Transfer

	// At is the timestamp of the last line parsed, not the time it was read.
	// A log that has stopped moving says so by this standing still.
	At time.Time

	// Outcome is how the run ended, in rclone's own words -- "successful",
	// "aborted", "interrupted by a signal" -- and is empty while it is still
	// going.
	Outcome  string
	Finished bool

	// ReadError says why this log could not be read, and is empty when it
	// could. A job that stands still because the file is unreadable looks
	// exactly like one that stands still because nothing is happening, and the
	// two mean opposite things.
	ReadError string

	Errors []LogLine
	Source Source
}

// Unit is a systemd service or timer that drives rclone.
//
// Most rclone runs on a schedule, and by the time anyone looks the job has
// exited. A process listing therefore cannot answer the question people
// actually have, which is "did my backup run, and did it work". The unit's
// recorded result can.
type Unit struct {
	Name  string
	Scope string // "user" or "system"

	ActiveState string // active, inactive, failed, activating
	SubState    string // running, dead, exited, start
	Result      string // success, exit-code, signal, timeout

	// ExitStatus is si_status from waitid, and ExitCode is si_code. The pair
	// has to be read together: ExitStatus is an exit code only when ExitCode
	// says the process exited (CLD_EXITED, 1). When it says the process was
	// killed (CLD_KILLED, 2) the same number is a signal, and reporting "exit
	// 15" for a unit systemd stopped normally is both the wrong quantity and a
	// false alarm.
	ExitStatus int
	ExitCode   string

	// InactiveExit is when the unit left the inactive state, which is when the
	// current or most recent run began. It is the only such record for a pure
	// Type=oneshot: systemd never sets ActiveEnterTimestamp for one.
	InactiveExit  time.Time
	ActiveEnter   time.Time
	InactiveEnter time.Time
	MainPID       int

	// Triggers is set on a timer: the unit it starts. LastTrigger and
	// NextElapse come from systemctl's own arithmetic rather than from the
	// boot-relative monotonic fields, which are empty for calendar timers.
	Triggers    string
	LastTrigger time.Time
	NextElapse  time.Time

	// LogFile is where this unit tells rclone to write, taken from its command
	// line or from the wrapper script it runs. It is what makes a scheduled job
	// followable between its runs, when no process exists to ask.
	LogFile string

	// Errors are the recent journal entries at warning severity or worse.
	Errors []LogLine

	Source Source
}

// IsTimer reports whether this unit schedules another.
func (u Unit) IsTimer() bool { return strings.HasSuffix(u.Name, ".timer") }

// waitid si_code values, as systemd reports them in ExecMainCode.
const (
	exitedNormally = "1" // CLD_EXITED: ExitStatus is an exit code
	killedBySignal = "2" // CLD_KILLED: ExitStatus is a signal number
	dumpedCore     = "3" // CLD_DUMPED
)

// Exit describes how the unit's main process ended, or an empty string when it
// ended in no notable way.
func (u Unit) Exit() string {
	switch u.ExitCode {
	case exitedNormally:
		if u.ExitStatus == 0 {
			return ""
		}
		return "exit " + strconv.Itoa(u.ExitStatus)
	case killedBySignal, dumpedCore:
		name := signalName(u.ExitStatus)
		if u.ExitCode == dumpedCore {
			return name + ", core dumped"
		}
		return "killed by " + name
	default:
		return ""
	}
}

// signalName renders the signals a long-running transfer actually meets. The
// rest are rare enough that the number is a better answer than a wrong name.
func signalName(n int) string {
	switch n {
	case 1:
		return "SIGHUP"
	case 2:
		return "SIGINT"
	case 9:
		return "SIGKILL"
	case 15:
		return "SIGTERM"
	default:
		return "signal " + strconv.Itoa(n)
	}
}

// Running reports whether the unit is up now.
//
// systemd holds a Type=oneshot unit at "activating" for the whole of its
// ExecStart, so a backup that is running right now is never "active". And a
// oneshot with RemainAfterExit=yes settles at active/exited, which systemd
// counts as active even though nothing is executing.
func (u Unit) Running() bool {
	return u.ActiveState == "activating" ||
		(u.ActiveState == "active" && u.SubState != "exited")
}

// Active reports whether systemd considers the unit active in any sub-state,
// including a finished oneshot held active by RemainAfterExit.
func (u Unit) Active() bool {
	return u.ActiveState == "active" || u.ActiveState == "activating" ||
		u.ActiveState == "reloading"
}

// LastRun is when the unit's most recent run began or ended.
//
// The choice is state-dependent. InactiveEnterTimestamp records the last
// transition into inactive, which for an active unit is a leftover from an
// earlier cycle and can be more recent than the run actually in progress.

func (u Unit) LastRun(timerTrigger time.Time) time.Time {
	if u.Active() {
		// The run in progress began when the unit left inactive. Preferring
		// ActiveEnter here would report nothing at all for a oneshot, and
		// falling through to InactiveEnter would time the gap since the
		// *previous* run -- a number that grows with every cycle of the timer.
		if !u.InactiveExit.IsZero() {
			return u.InactiveExit
		}
		if !u.ActiveEnter.IsZero() {
			return u.ActiveEnter
		}
	}
	if !u.InactiveEnter.IsZero() {
		return u.InactiveEnter
	}
	if !u.ActiveEnter.IsZero() {
		return u.ActiveEnter
	}
	return timerTrigger
}

// Failed reports whether the unit's last run ended badly. systemd keeps a
// oneshot job "inactive" whether it succeeded or not, so the state alone does
// not answer the question; Result does.
func (u Unit) Failed() bool {
	return u.ActiveState == "failed" || (u.Result != "" && u.Result != "success")
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
	Units     []Unit
	Jobs      []Job
	RCStats   []RCStats
}

// State is the merged view the UI renders, assembled from the most recent
// snapshot of each collector.
type State struct {
	Processes []Process
	Mounts    []Mount
	Caches    []CacheDir
	SyncPairs []SyncPair
	Units     []Unit
	Jobs      []Job
	RCStats   []RCStats

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
	if snap.Units != nil {
		s.Units = snap.Units
	}
	if snap.Jobs != nil {
		s.Jobs = snap.Jobs
	}
	if snap.RCStats != nil {
		s.RCStats = snap.RCStats
	}
	s.Seen[snap.Source] = snap.At
	delete(s.Errors, snap.Source)
}

// Fail records that a collector failed, without discarding the data it
// reported earlier. Stale data clearly marked as stale beats an empty screen.
func (s *State) Fail(src Source, err error) {
	s.Errors[src] = err
}
