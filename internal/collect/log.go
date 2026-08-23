package collect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

const (
	// logTailWindow is how far back a first read reaches into a file that was
	// already there. A log file accumulates for as long as the job has existed
	// -- months, on a nightly backup -- and none of that describes what is
	// happening now, which is the only thing this monitor claims to show.
	logTailWindow = 64 << 10

	// maxLogRead bounds a single tick. A run at DEBUG level can write faster
	// than any polling interval; when that happens the tail jumps forward to
	// the newest window rather than falling further behind on every tick.
	maxLogRead = 1 << 20

	// logRetention is how long a job survives after the process writing it has
	// gone. Long enough to read the last statistics block and the line saying
	// how the run ended, short enough that a host naming its log files by date
	// does not accumulate them for ever.
	logRetention = time.Hour
)

// logSource is what the process table knows about a log file: who is writing
// it, and what they are doing.
type logSource struct {
	pid  int
	kind model.Kind
}

// Logs follows the log files rclone is already writing.
//
// It is the only collector that can report progress against a known total while
// a run is in flight: /proc measures bytes through the kernel and knows nothing
// of how many files are still to come, and systemd only has the result once it
// is over. It is also the only source for a bisync pair's real paths, which the
// listing filenames mangle beyond recovery.
//
// Nothing here is configured. The files are discovered from the --log-file
// arguments of the processes the /proc collector has already found, which means
// rclonetop follows exactly the logs that are being written now, and never goes
// looking for files it was not pointed at.
type Logs struct {
	// mu guards known, which is written from the process collector's goroutine
	// and read from this one. The tails below are touched only by Collect and
	// need no lock; the file reads therefore happen outside it, so a slow disk
	// cannot hold up the one-second process tick.
	mu    sync.Mutex
	known map[string]logSource

	tails     map[string]*logTail
	observers []func(path1, path2 string)
}

// NewLogs returns a collector with nothing to follow yet. It learns what to
// read from NoteProcesses.
func NewLogs() *Logs {
	return &Logs{
		known: make(map[string]logSource),
		tails: make(map[string]*logTail),
	}
}

func (l *Logs) Name() string         { return "logs" }
func (l *Logs) Source() model.Source { return model.SourceLog }

// Interval is a compromise between the two clocks this collector sits between:
// reading a few kilobytes is cheap enough to do often, but rclone only writes
// its statistics on its own --stats interval, a minute by default, so polling
// faster than this buys nothing but wakeups.
func (l *Logs) Interval() time.Duration { return 2 * time.Second }

// Available is always true, and cannot be anything else: the collectors are
// filtered once at startup, before any process has been seen, so answering "no
// log files yet" there would switch this collector off for the whole session.
func (l *Logs) Available() bool { return true }

// OnPaths registers a callback invoked with each bisync pair whose real paths
// the log has revealed.
//
// This is how the bisync collector learns what its listing filenames stand for.
// It mirrors the process collector's OnProcesses, and for the same reason: one
// collector holds a fact that only another can use.
func (l *Logs) OnPaths(fn func(path1, path2 string)) {
	l.observers = append(l.observers, fn)
}

// NoteProcesses learns which log files are being written, and by whom.
func (l *Logs) NoteProcesses(procs []model.Process) {
	known := make(map[string]logSource, len(procs))
	for _, p := range procs {
		if path := logFileFromArgs(p.Args); path != "" {
			known[path] = logSource{pid: p.PID, kind: p.Kind}
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.known = known
}

// logFileFromArgs finds the log file in a command line, in either of the two
// spellings Go's flag package and rclone's both accept.
//
// RCLONE_LOG_FILE would do the same job, but a process's environment is not
// readable for another user's process and is not worth the special case: a job
// configured that way simply is not followed.
func logFileFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case a == "--log-file" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--log-file="):
			return strings.TrimPrefix(a, "--log-file=")
		}
	}
	return ""
}

func (l *Logs) Collect(ctx context.Context) (model.Snapshot, error) {
	now := time.Now()

	l.mu.Lock()
	known := make(map[string]logSource, len(l.known))
	for path, src := range l.known {
		known[path] = src
	}
	l.mu.Unlock()

	for path := range known {
		if _, ok := l.tails[path]; !ok {
			l.tails[path] = &logTail{
				path: path,
				job:  model.Job{LogFile: path, Source: model.SourceLog},
			}
		}
	}

	// Non-nil even when empty: a nil slice means "nothing to say", which would
	// leave the last job on screen after it finished.
	jobs := make([]model.Job, 0, len(l.tails))
	var firstErr error

	for path, tail := range l.tails {
		if err := ctx.Err(); err != nil {
			return model.Snapshot{}, err
		}

		src, live := known[path]
		if !live && tail.job.At.Before(now.Add(-logRetention)) {
			delete(l.tails, path)
			continue
		}

		if err := tail.read(); err != nil {
			if os.IsNotExist(err) {
				// The file was removed. There is nothing left to follow and
				// nothing to report about it.
				delete(l.tails, path)
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}

		// The command line is the better authority on what a job is: the log
		// only says so for bisync, and only once it has got that far.
		if live && src.kind != "" && src.kind != model.KindUnknown {
			tail.job.Kind = src.kind
		}
		// Zero for a job whose process has gone, which is what tells a run in
		// flight from the record of one that ended.
		tail.job.PID = src.pid

		job := tail.job
		// A copy, so the UI cannot be handed a slice this collector goes on
		// compacting in place as entries age out. It holds the last snapshot
		// until the next one arrives, and rewriting an error underneath it
		// would change what is on screen without anything having happened.
		job.Errors = append([]model.LogLine(nil), tail.job.Errors...)
		jobs = append(jobs, job)
	}

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].LogFile < jobs[j].LogFile })

	for _, job := range jobs {
		if job.Path1 == "" || job.Path2 == "" {
			continue
		}
		for _, fn := range l.observers {
			fn(job.Path1, job.Path2)
		}
	}

	return model.Snapshot{At: now, Source: model.SourceLog, Jobs: jobs}, firstErr
}

// read consumes whatever has been appended since the last call.
func (t *logTail) read() error {
	f, err := os.Open(t.path)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	// A named pipe would block on read and never end. Only a real file can be
	// tailed by offset anyway.
	if !info.Mode().IsRegular() {
		return nil
	}

	switch {
	case t.info == nil:
		// First sight of a file that has been there all along: start near its
		// end, and throw away the line that straddles the seek.
		if t.offset = info.Size() - logTailWindow; t.offset < 0 {
			t.offset = 0
		}
		t.skipPartial = t.offset > 0

	case !os.SameFile(info, t.info):
		// A different file wearing the same name: logrotate moved the old one
		// aside. The new one starts from its beginning, however much of it has
		// already been written.
		t.offset, t.skipPartial, t.pending = 0, false, nil

	case info.Size() < t.offset:
		// Truncated in place, which is the other way logrotate can work.
		t.offset, t.skipPartial, t.pending = 0, false, nil

	case info.Size()-t.offset > maxLogRead:
		// Written faster than this collector reads. Skipping to the newest
		// window loses the middle, which is the right thing to lose: what is
		// on screen should be what is happening now.
		t.offset, t.skipPartial = info.Size()-logTailWindow, true
	}
	t.info = info

	if info.Size() <= t.offset {
		return nil
	}

	size := info.Size() - t.offset
	if size > maxLogRead {
		size = maxLogRead
	}
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, t.offset)
	if err != nil && err != io.EOF {
		return err
	}
	buf = buf[:n]

	// Only whole lines are parsed: rclone may be halfway through writing the
	// last one, and half a line parses as nothing or, worse, as something else.
	end := bytes.LastIndexByte(buf, '\n')
	if end < 0 {
		// A line longer than the whole read budget. Skipping it is the only way
		// out; waiting for its end would stall the tail for good.
		if int64(n) >= maxLogRead {
			t.offset += int64(n)
			t.skipPartial = true
		}
		return nil
	}
	t.offset += int64(end) + 1

	for _, line := range strings.Split(string(buf[:end]), "\n") {
		if t.skipPartial {
			t.skipPartial = false
			continue
		}
		t.consume(line)
	}
	return nil
}

// plainHeader matches the prefix rclone puts on every line of a text log:
// the date, the time, and the level padded out to six columns.
//
// The fractional seconds are optional because --log-format can ask for
// microseconds, and the level list is rclone's own -- it has no WARNING, using
// NOTICE where another program would warn.
var plainHeader = regexp.MustCompile(
	`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) (DEBUG|INFO|NOTICE|WARNING|ERROR|CRITICAL)\s*: ?(.*)$`)

// plainLayouts are the two shapes the timestamp above can take. There is no
// zone in either: rclone writes local time, so that is how it must be read.
var plainLayouts = []string{"2006/01/02 15:04:05", "2006/01/02 15:04:05.999999"}

// logTail is the parse of one log file: the job it describes, and whatever
// block of statistics is currently half-read.
type logTail struct {
	job model.Job

	// pending is the statistics block being accumulated. rclone writes one as
	// several consecutive lines, and half of one is not a measurement:
	// committing early would replace a complete sample with a partial one for
	// as long as it took the rest to arrive.
	pending *model.JobStats

	// at is the timestamp of the last header line seen, carried across the
	// continuation lines that follow it and have none of their own.
	at time.Time

	// path, offset and info are the tail's position in the file. info is what
	// os.SameFile compares to notice that the name now refers to a different
	// file, which no amount of arithmetic on the offset could detect.
	path   string
	offset int64
	info   os.FileInfo

	// skipPartial discards the first line after a seek that did not land on a
	// line boundary, which is every seek that is not to the start of a file.
	skipPartial bool
}

// jsonEntry is one line of a --use-json-log file.
//
// The fields not named here are ignored on purpose: an entry carries whatever
// the call site attached to it, and this collector only needs the four that
// every entry has plus the statistics object.
type jsonEntry struct {
	Time  string     `json:"time"`
	Level string     `json:"level"`
	Msg   string     `json:"msg"`
	Stats *jsonStats `json:"stats"`
}

// jsonStats is rclone's accounting as it appears beside the stats message.
//
// This is the same data the text block spells out, except that it has not been
// rounded to three decimals on the way out, which is why it is preferred
// wherever it appears.
type jsonStats struct {
	Bytes          uint64  `json:"bytes"`
	TotalBytes     uint64  `json:"totalBytes"`
	Transfers      int     `json:"transfers"`
	TotalTransfers int     `json:"totalTransfers"`
	Checks         int     `json:"checks"`
	TotalChecks    int     `json:"totalChecks"`
	Errors         int     `json:"errors"`
	FatalError     bool    `json:"fatalError"`
	Deletes        int     `json:"deletes"`
	Renames        int     `json:"renames"`
	Speed          float64 `json:"speed"`
	ElapsedTime    float64 `json:"elapsedTime"`

	// ETA is a pointer because rclone writes null whenever it cannot estimate,
	// and a nil pointer is the only way to tell that apart from an estimate of
	// zero seconds, which means the opposite.
	ETA *float64 `json:"eta"`
}

// consume parses one line of the log.
//
// The format is detected per line rather than per file: one log file collects
// every run of the job that writes it, and nothing stops two of those runs
// having been started with different flags.
func (t *logTail) consume(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.HasPrefix(strings.TrimSpace(line), "{") && t.consumeJSON(line) {
		return
	}

	at, level, msg, ok := parsePlainHeader(line)
	if ok {
		// A header whose timestamp did not parse is still a header, but its
		// time is unknown. Taking the zero time from it would date every error
		// that follows to the year one and age the job out of the display.
		if !at.IsZero() {
			t.at = at
			t.job.At = at
		}
	} else {
		// A continuation line belongs to the entry above it: the statistics
		// block, or a message rclone chose to write across several lines.
		level, msg = "", line
	}
	t.apply(level, msg)
}

// apply routes one message, header stripped, to whatever it describes.
func (t *logTail) apply(level, msg string) {
	t.narrative(msg)
	t.statsLine(msg)
	t.recordError(level, msg)
}

// consumeJSON parses a structured entry, reporting false when the line is not
// one. A text log can contain a line that opens with a brace -- bisync prints
// its comparison settings as pretty JSON -- so failing to parse is normal and
// means "read this as text", not "this file is corrupt".
func (t *logTail) consumeJSON(line string) bool {
	var e jsonEntry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		return false
	}
	if e.Time == "" || e.Level == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, e.Time)
	if err != nil {
		return false
	}
	t.at = at
	t.job.At = at

	// A message can hold the whole statistics block, newlines and all. Only its
	// first line is the message proper; the rest are the block.
	level := strings.ToUpper(e.Level)
	for i, l := range strings.Split(e.Msg, "\n") {
		t.narrative(l)
		if e.Stats == nil {
			// Falling back to the text is worth it: an older rclone, or one
			// whose stats went to a different level, still says how far along
			// it is, just less precisely.
			t.statsLine(l)
		}
		if i == 0 {
			t.recordError(level, l)
		}
	}

	if e.Stats != nil {
		t.commit(e.Stats.model())
	}
	return true
}

// model converts rclone's own accounting into the shared vocabulary.
func (s jsonStats) model() model.JobStats {
	stats := model.JobStats{
		Bytes:          s.Bytes,
		TotalBytes:     s.TotalBytes,
		Transfers:      s.Transfers,
		TotalTransfers: s.TotalTransfers,
		Checks:         s.Checks,
		TotalChecks:    s.TotalChecks,
		Errors:         s.Errors,
		FatalError:     s.FatalError,
		Deletes:        s.Deletes,
		Renames:        s.Renames,
		Speed:          s.Speed,
		Elapsed:        time.Duration(s.ElapsedTime * float64(time.Second)),
	}
	if s.ETA != nil {
		stats.ETA, stats.ETAKnown = time.Duration(*s.ETA*float64(time.Second)), true
	}
	return stats
}

// bisyncPaths matches the two lines that write a pair's operands out in full.
// bisync says "with" when it starts and "vs" when it validates; either will do,
// and the second is what rescues a tail that began mid-run.
var bisyncPaths = regexp.MustCompile(`Path1 "(.+?)" (?:with|vs) Path2 "(.+)"$`)

// narrative reads the lines rclone writes about the run itself rather than
// about the files it is moving.
func (t *logTail) narrative(msg string) {
	if m := bisyncPaths.FindStringSubmatch(msg); m != nil {
		t.job.Path1, t.job.Path2 = m[1], m[2]
		// Only bisync writes this, so it also settles what kind of job the log
		// belongs to when the command line is not to hand.
		t.job.Kind = model.KindBisync
		if strings.HasPrefix(msg, "Synching ") {
			// This is bisync's first act, and therefore the clearest marker
			// that a new run has begun in a file full of old ones.
			t.newRun()
		}
		return
	}

	switch {
	case msg == "Bisync successful":
		t.finish("successful")
	case strings.HasPrefix(msg, "Bisync aborted"):
		t.finish("aborted")
	case strings.HasPrefix(msg, "Signal received: "):
		// rclone logs this on its way out, and it is the only trace a job
		// killed by a timeout or a systemctl stop leaves behind. Without it the
		// run simply stops mid-sentence and looks like a crash.
		t.finish("interrupted (" + strings.TrimPrefix(msg, "Signal received: ") + ")")
	}
}

// commit makes a complete sample the job's current statistics.
func (t *logTail) commit(stats model.JobStats) {
	// Elapsed time counts up within a run, so a smaller figure than the last
	// one can only mean a different run. It is the sole marker for a sync or a
	// copy, neither of which announces itself the way bisync does, and without
	// it a job that was interrupted this morning is still described as
	// interrupted while its successor is running.
	if t.job.HaveStats && stats.Elapsed < t.job.Stats.Elapsed {
		t.newRun()
	}

	t.job.Stats = stats
	t.job.HaveStats = true
	t.pending = nil
}

// finish records how the run ended.
func (t *logTail) finish(outcome string) {
	t.job.Outcome = outcome
	t.job.Finished = true
}

// newRun resets what belonged to the previous run in the same file.
//
// Log files are append-only across runs -- one file holds every bisync since
// the job was set up -- so without this the failure from this morning is still
// presented as the current state tonight.
func (t *logTail) newRun() {
	t.job.Outcome = ""
	t.job.Finished = false
	t.job.Errors = nil
	t.job.HaveStats = false
	t.job.Stats = model.JobStats{}
	t.pending = nil
}

// recordError keeps the lines worth showing: rclone has no WARNING level, so
// NOTICE is what it warns with, and everything below that is the running
// commentary of a transfer rather than a problem with it.
func (t *logTail) recordError(level, msg string) {
	// A statistics block opens with a header line carrying no message at all,
	// and at NOTICE level whenever --stats-log-level says so -- which is the
	// default. Recording those would fill the display with blank alarms.
	if strings.TrimSpace(msg) == "" {
		return
	}

	var priority int
	switch level {
	case "ERROR", "CRITICAL":
		priority = 3
	case "NOTICE", "WARNING":
		priority = 4
	default:
		return
	}

	t.job.Errors = append(t.job.Errors, model.LogLine{
		At:       t.at,
		Priority: priority,
		Message:  msg,
	})

	// Bounded the same way the journal's are, and for the same reasons: one
	// failing transfer can log thousands of lines, and a stale one is history
	// rather than a signal.
	cutoff := t.at.Add(-errorRetention)
	kept := t.job.Errors[:0]
	for _, e := range t.job.Errors {
		if e.At.After(cutoff) {
			kept = append(kept, e)
		}
	}
	if len(kept) > maxUnitErrors {
		kept = kept[len(kept)-maxUnitErrors:]
	}
	t.job.Errors = kept
}

// parsePlainHeader splits a text log line into its timestamp, level and
// message. It reports false for a continuation line, which has none of them.
func parsePlainHeader(line string) (at time.Time, level, msg string, ok bool) {
	m := plainHeader.FindStringSubmatch(line)
	if m == nil {
		return time.Time{}, "", "", false
	}
	for _, layout := range plainLayouts {
		if at, err := time.ParseInLocation(layout, m[1], time.Local); err == nil {
			return at, m[2], m[3], true
		}
	}
	return time.Time{}, m[2], m[3], true
}

// statsLine folds one line of a statistics block into the pending sample.
//
// The block is written by rclone's accounting and always opens with the byte
// counts and closes with the elapsed time:
//
//	Transferred:   	  731.185 MiB / 4.932 GiB, 14%, 12.408 MiB/s, ETA 5m48s
//	Errors:                 8 (fatal error encountered)
//	Checks:              9354 / 9354, 100%, Listed 11370
//	Transferred:          501 / 4667, 11%
//	Elapsed time:      5m14.6s
//
// Note that "Transferred:" appears twice and means something different each
// time: bytes on the first, files on the second. They are told apart by whether
// the two operands carry units.
func (t *logTail) statsLine(msg string) {
	label, rest, ok := strings.Cut(msg, ":")
	if !ok {
		return
	}
	rest = strings.TrimSpace(rest)

	switch label {
	case "Transferred":
		left, right, ok := splitProgress(rest)
		if !ok {
			return
		}
		if done, total, ok := parseCounts(left, right); ok {
			// The files line. It only appears once anything has actually
			// transferred, so it cannot be the one that opens the block.
			if t.pending != nil {
				t.pending.Transfers, t.pending.TotalTransfers = done, total
			}
			return
		}
		bytes, okB := parseLogSize(left)
		total, okT := parseLogSize(right)
		if !okB || !okT {
			return
		}
		// The bytes line opens a fresh block, discarding any earlier one that
		// never reached its "Elapsed time" and therefore never happened.
		t.pending = &model.JobStats{Bytes: bytes, TotalBytes: total}
		t.pending.Speed, t.pending.ETA, t.pending.ETAKnown = parseSpeedAndETA(rest)

	case "Checks":
		if t.pending == nil {
			return
		}
		left, right, ok := splitProgress(rest)
		if !ok {
			return
		}
		if done, total, ok := parseCounts(left, right); ok {
			t.pending.Checks, t.pending.TotalChecks = done, total
		}

	case "Errors":
		if t.pending == nil {
			return
		}
		if n, ok := leadingInt(rest); ok {
			t.pending.Errors = n
		}
		t.pending.FatalError = strings.Contains(rest, "fatal error")

	case "Deleted":
		if t.pending == nil {
			return
		}
		if n, ok := leadingInt(rest); ok {
			t.pending.Deletes = n
		}

	case "Renamed":
		if t.pending == nil {
			return
		}
		if n, ok := leadingInt(rest); ok {
			t.pending.Renames = n
		}

	case "Elapsed time":
		if t.pending == nil {
			return
		}
		if d, err := time.ParseDuration(rest); err == nil {
			t.pending.Elapsed = d
		}
		// The block is complete, so it becomes the sample.
		t.commit(*t.pending)
	}
}

// splitProgress separates the "done / total" operands that open a progress
// line, discarding the percentage and anything after it.
func splitProgress(rest string) (left, right string, ok bool) {
	head, _, _ := strings.Cut(rest, ",")
	left, right, ok = strings.Cut(head, "/")
	return strings.TrimSpace(left), strings.TrimSpace(right), ok
}

// parseCounts reads a pair of bare integers, which is what distinguishes a
// count of files from a quantity of bytes.
func parseCounts(left, right string) (done, total int, ok bool) {
	d, err1 := strconv.Atoi(left)
	tt, err2 := strconv.Atoi(right)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return d, tt, true
}

// leadingInt reads the number a line opens with, ignoring whatever annotation
// rclone puts in brackets after it.
func leadingInt(s string) (int, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(fields[0])
	return n, err == nil
}

// parseSpeedAndETA reads the rate and the estimate from the tail of the byte
// progress line, which rclone writes as ", 14%, 12.408 MiB/s, ETA 5m48s".
func parseSpeedAndETA(rest string) (speed float64, eta time.Duration, known bool) {
	for _, part := range strings.Split(rest, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasSuffix(part, "/s"):
			if n, ok := parseLogSize(strings.TrimSuffix(part, "/s")); ok {
				speed = float64(n)
			}
		case strings.HasPrefix(part, "ETA "):
			// rclone writes "-" whenever it cannot estimate, which is most of
			// a bisync. Zero there would read as "finished".
			value := strings.TrimSpace(strings.TrimPrefix(part, "ETA "))
			if d, err := time.ParseDuration(value); err == nil {
				eta, known = d, true
			}
		}
	}
	return speed, eta, known
}

// sizeUnits are the multipliers rclone prints sizes in. The binary ones are the
// default; the decimal ones appear when the run was started with --si.
var sizeUnits = map[string]float64{
	"B": 1,
	"K": 1 << 10, "Ki": 1 << 10, "KiB": 1 << 10,
	"M": 1 << 20, "Mi": 1 << 20, "MiB": 1 << 20,
	"G": 1 << 30, "Gi": 1 << 30, "GiB": 1 << 30,
	"T": 1 << 40, "Ti": 1 << 40, "TiB": 1 << 40,
	"P": 1 << 50, "Pi": 1 << 50, "PiB": 1 << 50,
	"kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15,
}

// parseLogSize reads a size as rclone writes it: a number, then a unit, with or
// without a space between them.
//
// The value is truncated rather than rounded. The log has already rounded it to
// three decimals, and rounding a rounded figure only invents precision that was
// never measured -- which is why the JSON format, where the byte count is
// exact, is preferred wherever it is available.
func parseLogSize(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}

	// The unit is the trailing run of letters; everything before it is the
	// number, whether or not they are separated.
	i := len(s)
	for i > 0 && (s[i-1] >= 'A' && s[i-1] <= 'Z' || s[i-1] >= 'a' && s[i-1] <= 'z') {
		i--
	}
	unit := s[i:]
	if unit == "" {
		return 0, false
	}
	mult, ok := sizeUnits[unit]
	if !ok {
		return 0, false
	}

	n, err := strconv.ParseFloat(strings.TrimSpace(s[:i]), 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return uint64(n * mult), true
}
