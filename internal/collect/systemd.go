package collect

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

const (
	// journalSeverity is the syslog level at or below which a journal entry is
	// worth showing: 4 is warning, 3 error. Anything chattier drowns the
	// signal.
	journalSeverity = 4

	// journalLines caps how much backlog is read the first time a unit is
	// seen, before the cursor takes over.
	journalLines = 50

	// execTimeout bounds any single systemctl or journalctl invocation.
	execTimeout = 10 * time.Second

	// wrapperScanLimit bounds how much of a wrapper script is read when
	// deciding whether it drives rclone.
	wrapperScanLimit = 256 << 10

	// errorRetention is how long a journal entry stays on screen. Past a day it
	// is history rather than a signal, and leaving it would keep a red mark
	// against a unit that has since been running cleanly.
	errorRetention = 24 * time.Hour

	// maxUnitErrors is how many recent journal entries are kept per unit.
	// Enough to show that a failure repeated, few enough that one noisy unit
	// cannot crowd out the rest of the display.
	maxUnitErrors = 5
)

// runner executes a command and returns its standard output. It is injected so
// the collector can be tested without a systemd on the other end.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// Systemd reports on the services and timers that drive rclone.
//
// This is the collector that answers the question people actually have. Most
// rclone runs on a schedule, and by the time anyone looks the job has exited:
// a process listing shows nothing, the logs have to be hunted for, and the one
// fact that matters -- did the last run succeed, and when is the next -- is
// recorded only by systemd.
//
// It shells out rather than speaking D-Bus. Both systemctl and journalctl emit
// JSON, so there is nothing to gain from a library that would add a dependency
// and a second way for the two to disagree.
type Systemd struct {
	run    runner
	scopes []string // "user", "system"

	// mu guards everything below. The maps are written from two goroutines:
	// the process collector's, which calls NoteProcesses on its own one-second
	// tick, and this collector's own five-second one. Without the lock Go's
	// runtime aborts the whole program on a concurrent map access, which takes
	// the TUI down with it.
	mu sync.Mutex

	// relevant caches which units matter, keyed by scope and name. Deciding
	// costs an exec and sometimes a file read, and a unit's identity does not
	// change between ticks.
	relevant map[string]bool

	// cursors is the journal position per unit, so each poll reads only what
	// has arrived since the last one.
	cursors map[string]string

	// recent retains what those polls turned up. Because the tail is
	// incremental, a poll returns only what is new; reporting just that would
	// make an error flash for one frame and then vanish, leaving a unit that
	// failed a minute ago looking clean.
	recent map[string][]model.LogLine

	checked   bool
	available bool
}

// NewSystemd returns a collector driving the real systemctl and journalctl.
func NewSystemd() *Systemd {
	return newSystemdWith(execRunner, []string{"user", "system"})
}

func newSystemdWith(run runner, scopes []string) *Systemd {
	return &Systemd{
		run:      run,
		scopes:   scopes,
		relevant: make(map[string]bool),
		cursors:  make(map[string]string),
		recent:   make(map[string][]model.LogLine),
	}
}

// execRunner runs a command, capturing enough of its diagnosis to report.
func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	// systemctl can take a moment on a busy host, and journalctl can block on a
	// damaged journal. Neither may stall a collector goroutine indefinitely.
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	// systemctl formats timestamps for a human unless told otherwise, and a
	// locale would change the month and day names. Nothing here is shown to
	// anyone, so ask for the machine-readable form throughout.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "SYSTEMD_COLORS=0")

	out, err := cmd.Output()
	if err != nil {
		// Without this the reason is thrown away and every failure reads the
		// same, which is the position the -d dump is meant to rescue people
		// from.
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%s: %s", err, firstLine(msg))
		}
		return nil, err
	}
	return out, nil
}

// firstLine keeps a diagnostic to something that fits on the footer.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func (s *Systemd) Name() string            { return "systemd" }
func (s *Systemd) Source() model.Source    { return model.SourceSystemd }
func (s *Systemd) Interval() time.Duration { return 5 * time.Second }

// Available reports whether systemctl answers at all. The result is cached: a
// host either has systemd or it does not.
func (s *Systemd) Available() bool {
	if s.checked {
		return s.available
	}
	s.checked = true

	_, err := s.run(context.Background(), "systemctl", "--version")
	s.available = err == nil
	return s.available
}

// noteUnit records that a unit is known to drive rclone.
//
// The process collector supplies these: a running rclone's cgroup names the
// unit that owns it, which is the only way to attribute a unit whose ExecStart
// points at a wrapper script that never mentions rclone by name.
func (s *Systemd) noteUnit(scope, unit string) {
	if unit == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relevant[scope+"/"+unit] = true
}

// NoteProcesses learns unit ownership from the processes already discovered.
//
// The cgroup is read by the process collector, which honours its own procfs
// root and therefore stays testable against a fixture, and which is already
// walking the directory anyway.
func (s *Systemd) NoteProcesses(procs []model.Process) {
	for _, p := range procs {
		if !strings.HasSuffix(p.Unit, ".service") {
			continue
		}
		// A cgroup path names the unit but not the scope it was defined in.
		// Recording both is what makes the lookup find whichever exists; the
		// cost of the wrong guess is one extra unit queried, which then simply
		// is not there.
		for _, scope := range s.scopes {
			s.noteUnit(scope, p.Unit)
		}
	}
}

func (s *Systemd) Collect(ctx context.Context) (model.Snapshot, error) {
	now := time.Now()
	// Non-nil even when empty, so a host without systemd clears the section
	// instead of leaving the last reading frozen on screen.
	units := []model.Unit{}

	if !s.Available() {
		return model.Snapshot{At: now, Source: model.SourceSystemd, Units: units}, nil
	}

	var failures []error
	for _, scope := range s.scopes {
		found, err := s.collectScope(ctx, scope)
		if err != nil {
			// One scope failing must not hide the other: a user session can
			// exist without a reachable system bus, and the reverse. But it
			// must not pass unremarked either -- units silently vanishing looks
			// exactly like having none, which is the confusion the whole
			// collector exists to prevent.
			failures = append(failures, fmt.Errorf("%s scope: %w", scope, err))
			continue
		}
		units = append(units, found...)
	}

	snap := model.Snapshot{At: now, Source: model.SourceSystemd, Units: units}
	if len(failures) == len(s.scopes) {
		// Every scope failed, so the empty list is not a finding. Reporting the
		// error keeps State.Fail from being unreachable and puts the reason in
		// the footer instead of leaving a blank section.
		return snap, errors.Join(failures...)
	}
	return snap, nil
}

func (s *Systemd) collectScope(ctx context.Context, scope string) ([]model.Unit, error) {
	services, err := s.listServices(ctx, scope)
	if err != nil {
		return nil, err
	}
	timers, err := s.listTimers(ctx, scope)
	if err != nil {
		// Timers are an enrichment; services alone are still worth showing.
		timers = map[string]timerInfo{}
	}

	s.prune(scope, services)
	wanted := s.classify(ctx, scope, services)

	// A timer matters when the unit it starts does. That is what puts "next
	// run in 12 minutes" next to a job that is not running now.
	for name, t := range timers {
		if wanted[t.Activates] {
			wanted[name] = true
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}
	sort.Strings(names)

	details, err := s.show(ctx, scope, names,
		"Id", "ActiveState", "SubState", "Result",
		"ExecMainCode", "ExecMainStatus",
		"InactiveExitTimestamp", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
		"MainPID")
	if err != nil {
		return nil, err
	}

	units := make([]model.Unit, 0, len(names))
	for _, name := range names {
		u := model.Unit{Name: name, Scope: scope, Source: model.SourceSystemd}

		if d, ok := details[name]; ok {
			u.ActiveState = d["ActiveState"]
			u.SubState = d["SubState"]
			u.Result = d["Result"]
			u.ExitCode = d["ExecMainCode"]
			u.ExitStatus, _ = strconv.Atoi(d["ExecMainStatus"])
			u.MainPID, _ = strconv.Atoi(d["MainPID"])
			if t, ok := parseUnixTimestamp(d["InactiveExitTimestamp"]); ok {
				u.InactiveExit = t
			}
			if t, ok := parseUnixTimestamp(d["ActiveEnterTimestamp"]); ok {
				u.ActiveEnter = t
			}
			if t, ok := parseUnixTimestamp(d["InactiveEnterTimestamp"]); ok {
				u.InactiveEnter = t
			}
		}
		if t, ok := timers[name]; ok {
			u.Triggers = t.Activates
			u.LastTrigger = t.Last
			u.NextElapse = t.Next
		}
		if !u.IsTimer() {
			u.Errors = s.journal(ctx, scope, name)
		}

		units = append(units, u)
	}
	return units, nil
}

// prune forgets units that are no longer listed.
//
// Without it the caches grow for the life of the process on a host that churns
// through templated or transient units -- a user session spawns a fresh
// dbus-:1.N-... service for every connection -- and a unit file edited and
// reloaded would keep its stale classification forever.
func (s *Systemd) prune(scope string, services []string) {
	live := make(map[string]bool, len(services))
	for _, name := range services {
		live[scope+"/"+name] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.relevant {
		if !strings.HasPrefix(key, scope+"/") {
			continue // another scope's entries, pruned on its own pass
		}
		if live[key] || strings.HasSuffix(key, ".timer") {
			continue // timers are not in the service listing
		}
		delete(s.relevant, key)
		delete(s.cursors, key)
		delete(s.recent, key)
	}
}

// classify decides which of the listed services drive rclone.
//
// There is no configuration to consult and no marker to look for, so it works
// from three independent signals, any one of which is enough:
//
//   - the unit was already attributed by a running process's cgroup;
//   - its name says so;
//   - its ExecStart command line names rclone.
//
// None of those catch the common arrangement where a unit runs a wrapper
// script and the script runs rclone, so as a last resort the script itself is
// read. That is bounded to small files that begin with a shebang, which keeps
// it from grepping through binaries.
func (s *Systemd) classify(ctx context.Context, scope string, services []string) map[string]bool {
	wanted := make(map[string]bool)

	var undecided []string
	s.mu.Lock()
	for _, name := range services {
		key := scope + "/" + name
		if known, seen := s.relevant[key]; seen {
			if known {
				wanted[name] = true
			}
			continue
		}
		if strings.Contains(name, "rclone") {
			s.relevant[key] = true
			wanted[name] = true
			continue
		}
		undecided = append(undecided, name)
	}
	s.mu.Unlock()
	if len(undecided) == 0 {
		return wanted
	}

	execs, err := s.show(ctx, scope, undecided, "Id", "ExecStart")
	if err != nil {
		return wanted
	}

	for _, name := range undecided {
		d := execs[name]
		hit := execStartMentionsRclone(d["ExecStart"])
		if !hit {
			for _, p := range execStartPaths(d["ExecStart"]) {
				if scriptMentionsRclone(p) {
					hit = true
					break
				}
			}
		}
		// Never downgrade. Classification takes over a second, and in that
		// window the process collector may have attributed this very unit from
		// a live rclone's cgroup -- the one signal that catches a wrapper
		// script. Overwriting it with a negative would hide the unit for good,
		// because a negative verdict is cached and never revisited.
		s.mu.Lock()
		hit = hit || s.relevant[scope+"/"+name]
		s.relevant[scope+"/"+name] = hit
		s.mu.Unlock()
		if hit {
			wanted[name] = true
		}
	}
	return wanted
}

// listServices returns the loaded service units in a scope.
func (s *Systemd) listServices(ctx context.Context, scope string) ([]string, error) {
	out, err := s.run(ctx, "systemctl", s.scopeFlag(scope),
		"list-units", "--type=service", "--all", "--no-pager", "--output=json")
	if err != nil {
		return nil, err
	}

	var listed []struct {
		Unit string `json:"unit"`
		Load string `json:"load"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(listed))
	for _, u := range listed {
		// not-found units are leftovers of something uninstalled: they have no
		// ExecStart to inspect and no state worth reporting.
		if u.Load == "not-found" {
			continue
		}
		names = append(names, u.Unit)
	}
	return names, nil
}

// timerInfo is what systemctl already worked out about a timer.
type timerInfo struct {
	Activates string
	Next      time.Time
	Last      time.Time
}

func (s *Systemd) listTimers(ctx context.Context, scope string) (map[string]timerInfo, error) {
	out, err := s.run(ctx, "systemctl", s.scopeFlag(scope),
		"list-timers", "--all", "--no-pager", "--output=json")
	if err != nil {
		return nil, err
	}
	return parseTimersJSON(out)
}

// parseTimersJSON reads the timer list.
//
// The epochs are microseconds, and systemctl has already resolved the next
// elapse into an absolute instant. Deriving it instead from the monotonic
// property on the unit would mean boot-relative arithmetic, and that property
// is empty for calendar timers anyway.
func parseTimersJSON(out []byte) (map[string]timerInfo, error) {
	var listed []struct {
		Unit      string `json:"unit"`
		Activates string `json:"activates"`
		Next      *int64 `json:"next"`
		Last      *int64 `json:"last"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, err
	}

	timers := make(map[string]timerInfo, len(listed))
	for _, t := range listed {
		info := timerInfo{Activates: t.Activates}
		// Zero means never, and must not be rendered as 1970.
		if t.Next != nil && *t.Next > 0 {
			info.Next = time.UnixMicro(*t.Next)
		}
		if t.Last != nil && *t.Last > 0 {
			info.Last = time.UnixMicro(*t.Last)
		}
		timers[t.Unit] = info
	}
	return timers, nil
}

// show queries properties for several units in one call, returning them keyed
// by unit name.
func (s *Systemd) show(ctx context.Context, scope string, units []string, props ...string) (map[string]map[string]string, error) {
	args := []string{s.scopeFlag(scope), "show", "--no-pager", "--timestamp=unix"}
	for _, p := range props {
		args = append(args, "-p", p)
	}
	// A unit name may begin with a dash -- "-.mount" exists on every system --
	// and systemctl would read it as a flag. Verified: "show -p Id --version"
	// prints the systemd version instead of the unit, and -H or -M would
	// redirect the whole call to another host or machine.
	args = append(args, "--")
	args = append(args, units...)

	out, err := s.run(ctx, "systemctl", args...)
	if err != nil {
		return nil, err
	}

	byUnit := make(map[string]map[string]string, len(units))
	blocks := parseShowBlocks(string(out))
	for i, b := range blocks {
		name := b["Id"]
		if name == "" && i < len(units) {
			// Id was not among the requested properties, or systemd omitted
			// it; the blocks come back in the order they were asked for.
			name = units[i]
		}
		byUnit[name] = b
	}
	return byUnit, nil
}

// parseShowBlocks splits systemctl show output into one map per unit.
//
// Blocks are separated by a blank line. Values are split on the first equals
// sign only: ExecStart embeds an entire command line, equals signs included.
func parseShowBlocks(out string) []map[string]string {
	var blocks []map[string]string
	current := map[string]string{}

	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = map[string]string{}
			}
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		current[key] = value
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

// parseUnixTimestamp reads the "@<epoch>" form produced by --timestamp=unix.
//
// Asking systemctl for that form avoids parsing "Sat 2026-08-22 23:22:02 CEST",
// whose day and zone names depend on the locale. An empty value means the
// transition never happened, which is not an error.
func parseUnixTimestamp(v string) (time.Time, bool) {
	if !strings.HasPrefix(v, "@") {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(strings.TrimSpace(v[1:]), 10, 64)
	if err != nil || secs <= 0 {
		return time.Time{}, false
	}
	return time.Unix(secs, 0), true
}

// journal reads the entries a unit has logged since the last poll.
func (s *Systemd) journal(ctx context.Context, scope, unit string) []model.LogLine {
	key := scope + "/" + unit
	args := []string{s.scopeFlag(scope), "-u", unit, "--no-pager", "-o", "json",
		"--output-fields=MESSAGE,PRIORITY"}
	s.mu.Lock()
	cursor, ok := s.cursors[key]
	s.mu.Unlock()
	if ok {
		args = append(args, "--after-cursor", cursor)
	}
	// Capped in both cases. Without it, a laptop resumed after a night asleep
	// asks for every entry since the cursor and buffers the lot: a single
	// ordinary unit's full history here is over two megabytes of JSON, and a
	// verbose rclone run is far more.
	args = append(args, "-n", strconv.Itoa(journalLines))

	out, err := s.run(ctx, "journalctl", args...)
	if err != nil {
		return nil
	}

	lines, next := parseJournalJSON(out, journalSeverity)
	s.mu.Lock()
	defer s.mu.Unlock()
	if next != "" {
		s.cursors[key] = next
	}
	return s.rememberLocked(key, lines)
}

// remember folds newly read entries into a unit's retained tail. The caller
// must not hold the lock.
func (s *Systemd) remember(key string, lines []model.LogLine) []model.LogLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rememberLocked(key, lines)
}

// rememberLocked is remember with the lock already held.
func (s *Systemd) rememberLocked(key string, lines []model.LogLine) []model.LogLine {
	cutoff := time.Now().Add(-errorRetention)

	kept := make([]model.LogLine, 0, len(s.recent[key])+len(lines))
	for _, l := range append(s.recent[key], lines...) {
		if l.At.IsZero() || l.At.After(cutoff) {
			kept = append(kept, l)
		}
	}
	if len(kept) > maxUnitErrors {
		kept = kept[len(kept)-maxUnitErrors:]
	}
	s.recent[key] = kept
	if len(kept) == 0 {
		return nil
	}
	// A copy, so the UI cannot be handed a slice this collector will append to
	// on the next tick.
	out := make([]model.LogLine, len(kept))
	copy(out, kept)
	return out
}

// parseJournalJSON reads journalctl's line-delimited output, keeping entries at
// severity or worse and returning the cursor of the last entry seen.
//
// The cursor advances past filtered-out entries too. Anchoring it to the last
// kept line instead would make every poll re-read and re-filter the same tail.
func parseJournalJSON(out []byte, severity int) ([]model.LogLine, string) {
	var lines []model.LogLine
	var cursor string

	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var entry struct {
			Cursor    string          `json:"__CURSOR"`
			Timestamp string          `json:"__REALTIME_TIMESTAMP"`
			Priority  string          `json:"PRIORITY"`
			Message   json.RawMessage `json:"MESSAGE"`
		}
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue // a rotated or corrupt entry must not lose the batch
		}
		if entry.Cursor != "" {
			cursor = entry.Cursor
		}

		priority, err := strconv.Atoi(entry.Priority)
		if err != nil || priority > severity {
			continue
		}
		message := decodeJournalMessage(entry.Message)
		if message == "" {
			continue
		}

		line := model.LogLine{Priority: priority, Message: message}
		if usec, err := strconv.ParseInt(entry.Timestamp, 10, 64); err == nil {
			line.At = time.UnixMicro(usec)
		}
		lines = append(lines, line)
	}
	return lines, cursor
}

// decodeJournalMessage handles both forms journald uses: a string, or an array
// of byte values when the message is not valid UTF-8.
func decodeJournalMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimRight(s, "\n")
	}
	var b []byte
	if err := json.Unmarshal(raw, &b); err == nil {
		return strings.TrimRight(string(b), "\n")
	}
	return ""
}

// execStartField matches the "path=" entries systemd writes in ExecStart.
var execStartField = regexp.MustCompile(`path=([^\s;]+)`)

// execStartArgv matches the argument vector systemd records alongside it.
var execStartArgv = regexp.MustCompile(`argv\[\]=([^;]*)`)

// execStartMentionsRclone reports whether the recorded command line names
// rclone.
func execStartMentionsRclone(execStart string) bool {
	return strings.Contains(execStart, "rclone")
}

// execStartPaths lists the files an ExecStart property refers to.
//
// The path= field alone is not enough: "ExecStart=/bin/sh /usr/local/bin/sync"
// records path=/bin/sh, and the script that actually drives rclone appears only
// in argv. Absolute arguments are therefore returned too.
func execStartPaths(execStart string) []string {
	var paths []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p != "" && path.IsAbs(p) && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, m := range execStartField.FindAllStringSubmatch(execStart, -1) {
		add(m[1])
	}
	for _, m := range execStartArgv.FindAllStringSubmatch(execStart, -1) {
		for _, arg := range strings.Fields(m[1]) {
			add(arg)
		}
	}
	return paths
}

// scriptMentionsRclone reads a wrapper script to see whether it drives rclone.
//
// This is what attributes a unit like jd-bisync.service, whose ExecStart names
// only a shell script. It is deliberately narrow: regular files under a size
// limit that begin with a shebang, so it never grinds through a binary.
func scriptMentionsRclone(p string) bool {
	if p == "" || !path.IsAbs(p) {
		return false
	}

	// Opened with O_NONBLOCK and inspected through the descriptor, never by
	// path. Stat-then-open would leave a window for the path to be swapped,
	// and opening a FIFO for reading blocks until a writer appears -- which
	// would wedge this collector's goroutine indefinitely. O_NONBLOCK makes
	// that open return instead, and the fstat below rejects it anyway.
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > wrapperScanLimit {
		return false
	}

	body := make([]byte, wrapperScanLimit)
	n, err := io.ReadFull(f, body)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return false
	}
	body = body[:n]

	// Only shell scripts. Without the shebang check this would grind through
	// any binary a unit happens to name, and match on an incidental string.
	if !bytes.HasPrefix(body, []byte("#!")) {
		return false
	}
	return bytes.Contains(body, []byte("rclone"))
}

// unitFromCgroup extracts the owning unit from /proc/<pid>/cgroup.
//
// This is the only signal that attributes a wrapper script's unit while it is
// actually running, because the rclone it spawns lands in the unit's own
// cgroup regardless of what the unit file says.
func unitFromCgroup(content string) string {
	var v1 string
	for _, line := range strings.Split(content, "\n") {
		// The unified hierarchy line is "0::<path>".
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			if leaf := cgroupLeaf(rest); leaf != "" {
				return leaf
			}
			continue
		}
		// A cgroup v1 host has no such line; the systemd hierarchy is named
		// instead, as "<id>:name=systemd:<path>". Without this a v1 host would
		// simply never attribute anything, and say nothing about why.
		if _, rest, ok := strings.Cut(line, ":name=systemd:"); ok {
			if leaf := cgroupLeaf(rest); leaf != "" && v1 == "" {
				v1 = leaf
			}
		}
	}
	return v1
}

func cgroupLeaf(p string) string {
	leaf := path.Base(strings.TrimSpace(p))
	if leaf == "" || leaf == "/" || leaf == "." {
		return ""
	}
	return leaf
}

func (s *Systemd) scopeFlag(scope string) string {
	if scope == "system" {
		return "--system"
	}
	return "--user"
}
