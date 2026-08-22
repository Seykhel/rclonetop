package collect

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

// userHZ is the kernel clock tick used by the starttime field of
// /proc/<pid>/stat. It is 100 on every Linux architecture in practice; the
// constant exists to make the arithmetic below readable rather than to be
// configurable.
const userHZ = 100

// remoteSpec matches an rclone remote reference such as "gdrive:" or
// "s3backup:bucket/path". The name may not contain a slash, which is what keeps
// absolute paths from matching.
var remoteSpec = regexp.MustCompile(`^[A-Za-z0-9_.\-]+:`)

// Procs discovers running rclone processes by reading /proc.
//
// This is the only collector that works everywhere, with no configuration and
// no cooperation from rclone itself, which is why it is the foundation the
// others build on. It is also the only way to measure the throughput of a FUSE
// mount that does not expose the rc API: the delta of the byte counters in
// /proc/<pid>/io is a real measurement of what the process moved.
type Procs struct {
	// root is the procfs mount point, overridable so tests can point at a
	// fixture directory instead of the live kernel.
	root string

	bootTime  time.Time
	prev      map[int]ioSample
	observers []func([]model.Process)
}

// ioSample is the previous reading of a process's byte counters, kept so the
// next reading can be turned into a rate.
type ioSample struct {
	at         time.Time
	startedAt  time.Time // guards against a recycled PID being read as a huge delta
	readTotal  uint64
	writeTotal uint64
}

// NewProcs returns a collector reading the live /proc.
func NewProcs() *Procs { return NewProcsAt("/proc") }

// NewProcsAt returns a collector reading procfs at root.
func NewProcsAt(root string) *Procs {
	return &Procs{root: root, prev: make(map[int]ioSample)}
}

func (p *Procs) Name() string            { return "procs" }
func (p *Procs) Source() model.Source    { return model.SourceProc }
func (p *Procs) Interval() time.Duration { return time.Second }

// Available reports whether procfs is readable. On Linux it always is; the
// check exists so the same binary can be built for platforms where it is not.
func (p *Procs) Available() bool {
	_, err := os.Stat(filepath.Join(p.root, "self", "stat"))
	return err == nil
}

func (p *Procs) Collect(ctx context.Context) (model.Snapshot, error) {
	now := time.Now()

	// The boot time is needed to turn a process's start tick into a wall
	// clock instant. It never changes, so it is read once and cached.
	if p.bootTime.IsZero() {
		bt, err := p.readBootTime()
		if err != nil {
			return model.Snapshot{}, err
		}
		p.bootTime = bt
	}

	entries, err := os.ReadDir(p.root)
	if err != nil {
		return model.Snapshot{}, err
	}

	// Non-nil even when empty. A nil slice means "this collector has nothing
	// to say", which State.Apply leaves alone; an empty one means "it looked
	// and found none". Returning nil here left the last rclone process frozen
	// on screen forever after it exited.
	procs := []model.Process{}
	seen := make(map[int]bool, len(p.prev))

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return model.Snapshot{}, err
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a process directory
		}
		proc, ok := p.readProcess(pid, now)
		if !ok {
			continue
		}
		seen[pid] = true
		procs = append(procs, proc)
	}

	// Drop the history of processes that have exited, so the map does not
	// grow without bound on a host that runs rclone on a timer.
	for pid := range p.prev {
		if !seen[pid] {
			delete(p.prev, pid)
		}
	}

	for _, fn := range p.observers {
		fn(procs)
	}

	return model.Snapshot{At: now, Source: model.SourceProc, Processes: procs}, nil
}

// readProcess builds a Process for pid, reporting false when it is not an
// rclone process or has exited mid-scan. A process disappearing between the
// directory listing and the read is normal, not an error.
func (p *Procs) readProcess(pid int, now time.Time) (model.Process, bool) {
	dir := filepath.Join(p.root, strconv.Itoa(pid))

	comm, err := os.ReadFile(filepath.Join(dir, "comm"))
	if err != nil || strings.TrimSpace(string(comm)) != "rclone" {
		return model.Process{}, false
	}

	args, err := readCmdline(filepath.Join(dir, "cmdline"))
	if err != nil || len(args) == 0 {
		return model.Process{}, false
	}

	proc := model.Process{
		PID:    pid,
		Source: model.SourceProc,
		Args:   args,
	}
	proc.Kind, proc.Remotes, proc.Paths, proc.Target = parseCmdline(args)
	proc.RCAddr = parseRCAddr(args)
	proc.StartedAt = p.startTime(dir)
	proc.RSS, proc.Threads = readStatus(filepath.Join(dir, "status"))
	if raw, err := os.ReadFile(filepath.Join(dir, "cgroup")); err == nil {
		proc.Unit = unitFromCgroup(string(raw))
	}

	rd, wr, ok := readIO(filepath.Join(dir, "io"))
	proc.IOAvailable = ok
	if ok {
		proc.ReadTotal, proc.WriteTotal = rd, wr
		proc.ReadRate, proc.WriteRate = p.rates(pid, proc, now)
		p.prev[pid] = ioSample{
			at:         now,
			startedAt:  proc.StartedAt,
			readTotal:  rd,
			writeTotal: wr,
		}
	}

	return proc, true
}

// rates converts the cumulative byte counters into bytes per second using the
// previous sample. It returns zeroes when there is no usable previous sample,
// which is the honest answer on the first frame.
func (p *Procs) rates(pid int, proc model.Process, now time.Time) (read, write float64) {
	prev, ok := p.prev[pid]
	if !ok {
		return 0, 0
	}
	// A PID can be recycled by the kernel. If the start time moved, this is
	// a different process wearing the same number and the delta is garbage.
	if !prev.startedAt.Equal(proc.StartedAt) {
		return 0, 0
	}
	elapsed := now.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}
	// Counters only grow, but a wrap or a bad read would produce a huge
	// negative delta, so clamp rather than render nonsense.
	if proc.ReadTotal >= prev.readTotal {
		read = float64(proc.ReadTotal-prev.readTotal) / elapsed
	}
	if proc.WriteTotal >= prev.writeTotal {
		write = float64(proc.WriteTotal-prev.writeTotal) / elapsed
	}
	return read, write
}

// readBootTime extracts the btime field from /proc/stat, the wall clock instant
// the kernel started counting ticks from.
func (p *Procs) readBootTime() (time.Time, error) {
	f, err := os.Open(filepath.Join(p.root, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		secs, err := strconv.ParseInt(strings.TrimSpace(line[len("btime "):]), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		return time.Unix(secs, 0), nil
	}
	return time.Time{}, sc.Err()
}

// startTime reads field 22 of /proc/<pid>/stat and converts it to wall clock.
//
// The file cannot simply be split on spaces: field 2 is the executable name in
// parentheses and may itself contain spaces. Everything after the final ')' is
// safe to split, and there field 3 is at index 0.
func (p *Procs) startTime(dir string) time.Time {
	raw, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return time.Time{}
	}
	close := bytes.LastIndexByte(raw, ')')
	if close < 0 || close+2 >= len(raw) {
		return time.Time{}
	}
	fields := strings.Fields(string(raw[close+2:]))
	const startTimeIndex = 19 // field 22, minus the pid, comm and state already consumed
	if len(fields) <= startTimeIndex {
		return time.Time{}
	}
	ticks, err := strconv.ParseInt(fields[startTimeIndex], 10, 64)
	if err != nil {
		return time.Time{}
	}
	return p.bootTime.Add(time.Duration(ticks) * time.Second / userHZ)
}

// readStatus pulls the resident set size and thread count out of
// /proc/<pid>/status. Missing fields yield zeroes rather than an error: they
// are decoration, not the reason the process is on screen.
func readStatus(path string) (rss uint64, threads int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			// Reported in kibibytes by the kernel.
			if kb, err := strconv.ParseUint(strings.Fields(line)[1], 10, 64); err == nil {
				rss = kb * 1024
			}
		case strings.HasPrefix(line, "Threads:"):
			if n, err := strconv.Atoi(strings.Fields(line)[1]); err == nil {
				threads = n
			}
		}
	}
	return rss, threads
}

// readIO returns the cumulative rchar and wchar counters. It reports false when
// the file cannot be read, which is the normal outcome for a process owned by
// another user and must not be confused with a process that moved no bytes.
func readIO(path string) (read, write uint64, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "rchar:":
			read, ok = v, true
		case "wchar:":
			write, ok = v, true
		}
	}
	return read, write, ok
}

// readCmdline splits the NUL-separated argument vector.
func readCmdline(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw = bytes.TrimRight(raw, "\x00")
	if len(raw) == 0 {
		return nil, nil
	}
	parts := bytes.Split(raw, []byte{0})
	args := make([]string, 0, len(parts))
	for _, p := range parts {
		args = append(args, string(p))
	}
	return args, nil
}

// subcommands maps every rclone subcommand to the kind of workload it
// represents. The ones that are not interesting to monitor still have to be
// listed: the map is also how the subcommand is told apart from a global flag's
// value, since "rclone --config /etc/rclone.conf rcd" puts a path exactly where
// the subcommand would otherwise be expected.
var subcommands = map[string]model.Kind{
	"mount": model.KindMount, "mount2": model.KindMount,
	"nfsmount": model.KindMount, "cmount": model.KindMount,

	"sync":   model.KindSync,
	"bisync": model.KindBisync,

	"copy": model.KindCopy, "copyto": model.KindCopy, "copyurl": model.KindCopy,
	"move": model.KindMove, "moveto": model.KindMove,

	"check": model.KindCheck, "cryptcheck": model.KindCheck, "checksum": model.KindCheck,

	"serve": model.KindServe,
	"rcd":   model.KindRCD,

	// Recognised so their positional arguments parse, but not a workload
	// worth its own colour.
	"about": model.KindUnknown, "authorize": model.KindUnknown, "backend": model.KindUnknown,
	"cat": model.KindUnknown, "cleanup": model.KindUnknown, "config": model.KindUnknown,
	"dedupe": model.KindUnknown, "delete": model.KindUnknown, "deletefile": model.KindUnknown,
	"gitannex": model.KindUnknown, "hashsum": model.KindUnknown, "link": model.KindUnknown,
	"listremotes": model.KindUnknown, "ls": model.KindUnknown, "lsd": model.KindUnknown,
	"lsf": model.KindUnknown, "lsjson": model.KindUnknown, "lsl": model.KindUnknown,
	"md5sum": model.KindUnknown, "mkdir": model.KindUnknown, "obscure": model.KindUnknown,
	"purge": model.KindUnknown, "rc": model.KindUnknown, "rmdir": model.KindUnknown,
	"rmdirs": model.KindUnknown, "selfupdate": model.KindUnknown, "settier": model.KindUnknown,
	"sha1sum": model.KindUnknown, "size": model.KindUnknown, "test": model.KindUnknown,
	"touch": model.KindUnknown, "tree": model.KindUnknown, "version": model.KindUnknown,
}

// parseCmdline works out what an rclone invocation is doing.
//
// The subcommand is found by matching against the known set rather than by
// taking the first non-flag argument, which would pick up the value of a global
// flag placed before it.
//
// Positional arguments are then the non-flag arguments that follow, up to the
// first flag. That is the documented form and the one every wrapper script
// uses. It does misread "rclone mount --vfs-cache-mode full remote: /mnt",
// where "full" would be taken as positional; recognising that would mean
// carrying rclone's entire flag table, which is not worth it for a display hint.
func parseCmdline(args []string) (kind model.Kind, remotes, paths []string, target string) {
	if len(args) < 2 {
		return model.KindUnknown, nil, nil, ""
	}

	subIndex := -1
	for i, a := range args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if k, ok := subcommands[a]; ok {
			subIndex, kind = i+1, k
			break
		}
	}
	if subIndex < 0 {
		return model.KindUnknown, nil, nil, ""
	}

	for _, a := range args[subIndex+1:] {
		if strings.HasPrefix(a, "-") {
			break // flags start here, positionals are done
		}
		paths = append(paths, a)
	}

	for _, a := range paths {
		if remoteSpec.MatchString(a) {
			remotes = append(remotes, a)
		}
	}
	// The last positional is the destination for a transfer and the
	// mountpoint for a mount. Either way it is what the process is acting on,
	// and for a transfer it stays in the remotes list too, on purpose.
	if len(paths) > 0 {
		target = paths[len(paths)-1]
	}

	return kind, remotes, paths, target
}

// parseRCAddr finds the address a process serves the rc API on. This is the
// only discovery mechanism rclonetop uses: it reads command lines that are
// already on the host and never probes the network, because access to the rc
// API is equivalent to shell access as the rclone user.
func parseRCAddr(args []string) string {
	for i, a := range args {
		switch {
		case a == "--rc-addr" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--rc-addr="):
			return strings.TrimPrefix(a, "--rc-addr=")
		}
	}
	// --rc and rcd both default to this address when no explicit one is set.
	for _, a := range args {
		if a == "--rc" || a == "rcd" {
			return "localhost:5572"
		}
	}
	return ""
}

// OnProcesses registers a callback invoked with each fresh set of discovered
// processes.
//
// It exists so the systemd collector can learn which units own rclone from
// their cgroups. That attribution is only possible while a process is alive,
// and only the process collector is looking at the right moment.
func (p *Procs) OnProcesses(fn func([]model.Process)) {
	p.observers = append(p.observers, fn)
}
