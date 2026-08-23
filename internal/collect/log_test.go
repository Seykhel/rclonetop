package collect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

func writeLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func appendLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatalf("appending to %s: %v", path, err)
		}
	}
}

// jobFor finds a snapshot's job by the file it came from, so the tests do not
// depend on the order they are reported in.
func jobFor(t *testing.T, snap model.Snapshot, path string) model.Job {
	t.Helper()
	for _, j := range snap.Jobs {
		if j.LogFile == path {
			return j
		}
	}
	t.Fatalf("no job for %s in %+v", path, snap.Jobs)
	return model.Job{}
}

func TestLogsDiscoverTheirFilesFromTheCommandLine(t *testing.T) {
	dir := t.TempDir()
	joined := filepath.Join(dir, "joined.log")
	separate := filepath.Join(dir, "separate.log")
	writeLog(t, joined, "2026/08/22 23:52:22 INFO  : Bisync successful")
	writeLog(t, separate, "2026/08/22 23:52:22 INFO  : Bisync successful")

	l := NewLogs()
	l.NoteProcesses([]model.Process{
		{PID: 11, Kind: model.KindBisync, Args: []string{"rclone", "bisync", "a", "b", "--log-file=" + joined}},
		{PID: 22, Kind: model.KindSync, Args: []string{"rclone", "sync", "a", "b", "--log-file", separate}},
		// No log file at all: nothing to follow, and no reason to invent one.
		{PID: 33, Kind: model.KindMount, Args: []string{"rclone", "mount", "gdrive:", "/mnt"}},
	})

	snap, err := l.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Jobs) != 2 {
		t.Fatalf("got %d jobs, want 2: %+v", len(snap.Jobs), snap.Jobs)
	}
	if got := jobFor(t, snap, joined); got.PID != 11 || got.Kind != model.KindBisync {
		t.Errorf("joined form: pid %d kind %q", got.PID, got.Kind)
	}
	if got := jobFor(t, snap, separate); got.PID != 22 || got.Kind != model.KindSync {
		t.Errorf("separate form: pid %d kind %q", got.PID, got.Kind)
	}
}

// The invariant the whole model rests on: an empty slice means "looked and
// found none", a nil one means "nothing to say", and only the first clears
// what is on screen.
func TestNoLogsReportsEmptyNotNil(t *testing.T) {
	snap, err := NewLogs().Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap.Jobs == nil {
		t.Error("Jobs is nil, which leaves a finished job on screen for ever")
	}
	if len(snap.Jobs) != 0 {
		t.Errorf("got %d jobs, want none", len(snap.Jobs))
	}
}

// The tail is incremental. Re-reading from the start would repeat every error
// the file has ever held, growing the display without anything happening.
func TestOnlyNewLinesAreRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclone.log")
	writeLog(t, path,
		"2026/08/22 17:45:22 ERROR : one.md: Failed to copy: RootURL not set",
		"2026/08/22 17:45:23 ERROR : two.md: Failed to copy: RootURL not set",
	)

	l := NewLogs()
	l.NoteProcesses([]model.Process{{PID: 11, Kind: model.KindSync,
		Args: []string{"rclone", "sync", "a", "b", "--log-file", path}}})

	snap, _ := l.Collect(context.Background())
	if got := len(jobFor(t, snap, path).Errors); got != 2 {
		t.Fatalf("got %d errors on the first pass, want 2", got)
	}

	// Nothing has changed, so nothing new should be read.
	snap, _ = l.Collect(context.Background())
	if got := len(jobFor(t, snap, path).Errors); got != 2 {
		t.Errorf("got %d errors on the second pass, want the same 2", got)
	}

	appendLog(t, path, "2026/08/22 17:45:24 ERROR : three.md: Failed to copy: RootURL not set")
	snap, _ = l.Collect(context.Background())
	if got := len(jobFor(t, snap, path).Errors); got != 3 {
		t.Errorf("got %d errors after appending one, want 3", got)
	}
}

// logrotate moves the file aside and rclone keeps writing to a new one with the
// same name. Following the offset alone would skip however much the new file
// had already been given.
func TestRotationIsFollowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclone.log")
	writeLog(t, path,
		"2026/08/22 17:45:22 ERROR : one.md: Failed to copy: some fairly long message to push the offset along",
		"2026/08/22 17:45:23 ERROR : two.md: Failed to copy: some fairly long message to push the offset along",
	)

	l := NewLogs()
	l.NoteProcesses([]model.Process{{PID: 11, Kind: model.KindSync,
		Args: []string{"rclone", "sync", "a", "b", "--log-file", path}}})
	l.Collect(context.Background())

	// Rotated: the old file is moved aside and a longer one takes its name, so
	// an offset carried over would land in the middle of it.
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatalf("rotating: %v", err)
	}
	writeLog(t, path,
		`2026/08/22 18:40:00 INFO  : Synching Path1 "/home/user/Documents/" with Path2 "gdrive:Documents/"`,
		"2026/08/22 18:40:47 INFO  : ",
		"Transferred:   \t          0 B / 0 B, -, 0 B/s, ETA -",
		"Checks:              9418 / 9418, 100%, Listed 11259",
		"Elapsed time:        47.6s",
		"",
		"2026/08/22 18:40:47 INFO  : Bisync successful",
	)

	snap, _ := l.Collect(context.Background())
	job := jobFor(t, snap, path)
	if job.Path1 != "/home/user/Documents/" {
		t.Errorf("path1 = %q: the head of the new file was skipped", job.Path1)
	}
	if !job.HaveStats || job.Stats.Checks != 9418 {
		t.Errorf("stats = %+v, want the new file's", job.Stats)
	}
	if len(job.Errors) != 0 {
		t.Errorf("the rotated-away errors are still attached: %+v", job.Errors)
	}
}

// The other rotation: the file is truncated in place and keeps its identity.
func TestTruncationIsFollowed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclone.log")
	writeLog(t, path,
		"2026/08/22 17:45:22 ERROR : one.md: Failed to copy: some fairly long message to push the offset along",
		"2026/08/22 17:45:23 ERROR : two.md: Failed to copy: some fairly long message to push the offset along",
	)

	l := NewLogs()
	l.NoteProcesses([]model.Process{{PID: 11, Kind: model.KindSync,
		Args: []string{"rclone", "sync", "a", "b", "--log-file", path}}})
	l.Collect(context.Background())

	writeLog(t, path, "2026/08/22 18:40:47 INFO  : Bisync successful")

	snap, _ := l.Collect(context.Background())
	if !jobFor(t, snap, path).Finished {
		t.Error("the line written after the truncation was not read")
	}
}

// A snapshot must not change under the UI after it has been handed over.
//
// The tail prunes its retained errors in place, and the UI holds the previous
// snapshot until the next one arrives -- so handing out the collector's own
// slice lets a later tick rewrite entries that are on screen now. The systemd
// collector already copies for exactly this reason.
func TestHandedOutErrorsAreNotRewrittenLater(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclone.log")
	writeLog(t, path, "2026/08/20 17:45:22 ERROR : old.md: Failed to copy: RootURL not set")

	l := NewLogs()
	l.NoteProcesses([]model.Process{{PID: 11, Kind: model.KindSync,
		Args: []string{"rclone", "sync", "a", "b", "--log-file", path}}})

	first, _ := l.Collect(context.Background())
	second, _ := l.Collect(context.Background())

	held := jobFor(t, first, path).Errors
	next := jobFor(t, second, path).Errors
	if len(held) != 1 || len(next) != 1 {
		t.Fatalf("got %d and %d errors, want 1 each", len(held), len(next))
	}

	// Sharing the array is the defect itself, whether or not a particular
	// sequence of appends happens to expose it: the tail compacts that array in
	// place as entries age out, and the UI is still reading the older snapshot
	// from it.
	if &held[0] == &next[0] {
		t.Error("two snapshots were handed the same backing array")
	}
}

// A log that has been running for months must not be read from the beginning:
// what matters is what is happening now, and the rest is history that would
// cost a great deal to walk through to reach it.
func TestOnlyTheTailOfALongLogIsRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclone.log")

	lines := []string{"2026/08/22 17:45:22 ERROR : ancient.md: Failed to copy: RootURL not set"}
	// Both errors carry the same timestamp, so age is not what separates them.
	for i := 0; i < 4000; i++ {
		lines = append(lines, "2026/08/22 17:45:22 INFO  : notes/file: Set directory modification time (using SetModTime)")
	}
	lines = append(lines, "2026/08/22 17:45:22 ERROR : recent.md: Failed to copy: RootURL not set")
	writeLog(t, path, lines...)

	l := NewLogs()
	l.NoteProcesses([]model.Process{{PID: 11, Kind: model.KindSync,
		Args: []string{"rclone", "sync", "a", "b", "--log-file", path}}})

	snap, _ := l.Collect(context.Background())
	job := jobFor(t, snap, path)
	if len(job.Errors) != 1 {
		t.Fatalf("got %d errors, want only the recent one: %+v", len(job.Errors), job.Errors)
	}
	if !strings.Contains(job.Errors[0].Message, "recent.md") {
		t.Errorf("kept the wrong error: %q", job.Errors[0].Message)
	}
}

// feed runs lines through a tail as if they had just been appended to a log
// file, and returns the job it built.
func feed(lines ...string) *logTail {
	t := &logTail{}
	for _, line := range lines {
		t.consume(line)
	}
	return t
}

// The blocks below are copied from a real rclone log, with the paths
// neutralised. Nothing about them is tidied: the tab after "Transferred:", the
// alignment padding and the trailing "Listed" count are all as rclone writes
// them.
func TestParsePlainStatsBlock(t *testing.T) {
	tail := feed(
		"2026/08/22 17:45:33 INFO  : ",
		"Transferred:   \t  731.185 MiB / 4.932 GiB, 14%, 12.408 MiB/s, ETA 5m48s",
		"Errors:                 8 (fatal error encountered)",
		"Checks:              9354 / 9354, 100%, Listed 11370",
		"Transferred:          501 / 4667, 11%",
		"Elapsed time:      5m14.6s",
		"",
	)

	if !tail.job.HaveStats {
		t.Fatal("the block should have produced statistics")
	}
	s := tail.job.Stats

	// 731.185 x 2^20 = 766703042, and 4.932 x 2^30 = 5295694675. The log has
	// already rounded to three decimals, so these are as exact as the text
	// format can be -- which is the reason the JSON format is preferred.
	if s.Bytes != 766703042 {
		t.Errorf("bytes = %d, want 766703042", s.Bytes)
	}
	if s.TotalBytes != 5295694675 {
		t.Errorf("total bytes = %d, want 5295694675", s.TotalBytes)
	}
	if s.Transfers != 501 || s.TotalTransfers != 4667 {
		t.Errorf("transfers = %d/%d, want 501/4667", s.Transfers, s.TotalTransfers)
	}
	if s.Checks != 9354 || s.TotalChecks != 9354 {
		t.Errorf("checks = %d/%d, want 9354/9354", s.Checks, s.TotalChecks)
	}
	if s.Errors != 8 {
		t.Errorf("errors = %d, want 8", s.Errors)
	}
	if !s.FatalError {
		t.Error("the block says the errors were fatal")
	}
	if s.Elapsed != 5*time.Minute+14600*time.Millisecond {
		t.Errorf("elapsed = %s, want 5m14.6s", s.Elapsed)
	}
	if s.ETA != 5*time.Minute+48*time.Second || !s.ETAKnown {
		t.Errorf("eta = %s (known %v), want 5m48s", s.ETA, s.ETAKnown)
	}
	// 12.408 x 2^20 = 13010731.
	if s.Speed < 13010730 || s.Speed > 13010732 {
		t.Errorf("speed = %f, want about 13010731", s.Speed)
	}
	if want := time.Date(2026, 8, 22, 17, 45, 33, 0, time.Local); !tail.job.At.Equal(want) {
		t.Errorf("timestamp = %s, want %s", tail.job.At, want)
	}
}

// An idle bisync writes a block with no transfers at all, and the dashes where
// a percentage and an ETA would be must not be read as zeroes.
func TestParsePlainStatsBlockWithNothingToDo(t *testing.T) {
	tail := feed(
		"2026/08/22 23:52:22 INFO  : ",
		"Transferred:   \t          0 B / 0 B, -, 0 B/s, ETA -",
		"Checks:              9418 / 9418, 100%, Listed 11259",
		"Elapsed time:        47.6s",
		"",
	)

	if !tail.job.HaveStats {
		t.Fatal("the block should have produced statistics")
	}
	s := tail.job.Stats
	if s.Bytes != 0 || s.TotalBytes != 0 {
		t.Errorf("bytes = %d/%d, want 0/0", s.Bytes, s.TotalBytes)
	}
	if s.ETAKnown {
		t.Error("an ETA of \"-\" is unknown, not zero")
	}
	if s.Checks != 9418 {
		t.Errorf("checks = %d, want 9418", s.Checks)
	}
	if s.Elapsed != 47600*time.Millisecond {
		t.Errorf("elapsed = %s, want 47.6s", s.Elapsed)
	}
}

// The paths are the reason this collector exists for bisync: the listing
// filenames mangle them beyond recovery, and the log writes them out in full.
func TestBisyncPathsAreRecoveredFromTheLog(t *testing.T) {
	tail := feed(
		`2026/08/22 12:51:34 INFO  : Setting --ignore-listing-checksum as neither --checksum nor --compare checksum are set.`,
		`2026/08/22 12:51:34 INFO  : Synching Path1 "/home/user/Documents/" with Path2 "gdrive:Documents/"`,
		`2026/08/22 12:51:34 INFO  : Using filters file /home/user/.config/rclone/jd-filter.txt`,
	)

	if tail.job.Path1 != "/home/user/Documents/" || tail.job.Path2 != "gdrive:Documents/" {
		t.Errorf("paths = %q / %q", tail.job.Path1, tail.job.Path2)
	}
	if tail.job.Kind != model.KindBisync {
		t.Errorf("kind = %q, want bisync: only bisync writes that line", tail.job.Kind)
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		lines    []string
		outcome  string
		finished bool
	}{
		{
			name:     "success",
			lines:    []string{"2026/08/22 23:52:22 INFO  : Bisync successful"},
			outcome:  "successful",
			finished: true,
		},
		{
			name: "aborted",
			lines: []string{
				"2026/08/22 17:45:33 ERROR : Bisync critical error: failed to set directory modtime: chtimes /home/user/Documents/notes: no such file or directory",
				"2026/08/22 17:45:33 ERROR : Bisync aborted. Must run --resync to recover.",
			},
			outcome:  "aborted",
			finished: true,
		},
		{
			// The run systemd reported as "code=killed, status=15". rclone
			// shuts down gracefully and says so, which is the only record that
			// the job did not simply stop.
			name: "terminated",
			lines: []string{
				"2026/08/22 18:51:56 INFO  : Signal received: terminated",
				"2026/08/22 18:51:56 NOTICE: Attempting to gracefully shutdown. (Send exit signal again for immediate un-graceful shutdown.)",
				"2026/08/22 18:51:56 NOTICE: Graceful shutdown completed successfully.",
				"2026/08/22 18:51:56 INFO  : Exiting...",
			},
			outcome:  "interrupted (terminated)",
			finished: true,
		},
		{
			name:     "still going",
			lines:    []string{"2026/08/22 18:51:33 INFO  : Building Path1 and Path2 listings"},
			outcome:  "",
			finished: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tail := feed(c.lines...)
			if tail.job.Outcome != c.outcome {
				t.Errorf("outcome = %q, want %q", tail.job.Outcome, c.outcome)
			}
			if tail.job.Finished != c.finished {
				t.Errorf("finished = %v, want %v", tail.job.Finished, c.finished)
			}
		})
	}
}

func TestErrorsAreKeptWarningAndWorseOnly(t *testing.T) {
	tail := feed(
		`2026/08/22 17:45:22 INFO  : 10-19 Vita: Set directory modification time`,
		`2026/08/22 17:45:22 DEBUG : notes/todo.md: Sending chunk`,
		`2026/08/22 17:45:22 ERROR : notes/todo.md: Failed to copy: failed to open source object: RootURL not set`,
		`2026/08/22 17:45:33 NOTICE: Failed to bisync with 8 errors: last error was: bisync aborted`,
	)

	if len(tail.job.Errors) != 2 {
		t.Fatalf("got %d errors, want the ERROR and the NOTICE only:\n%+v", len(tail.job.Errors), tail.job.Errors)
	}
	if got := tail.job.Errors[0]; got.Priority != 3 {
		t.Errorf("an ERROR should be priority 3, got %d", got.Priority)
	}
	// rclone has no WARNING level; NOTICE is what it warns with.
	if got := tail.job.Errors[1]; got.Priority != 4 {
		t.Errorf("a NOTICE should be priority 4, got %d", got.Priority)
	}
	if want := time.Date(2026, 8, 22, 17, 45, 22, 0, time.Local); !tail.job.Errors[0].At.Equal(want) {
		t.Errorf("error timestamp = %s, want %s", tail.job.Errors[0].At, want)
	}
}

// A log file is append-only across runs: the same file holds every bisync since
// the job was set up. Without noticing where one run ends and the next begins,
// a failure from this morning is still reported as the current state hours
// later, which is exactly the confusion this collector is meant to remove.
func TestANewRunClearsTheLastOne(t *testing.T) {
	tail := feed(
		"2026/08/22 17:45:33 ERROR : Bisync aborted. Must run --resync to recover.",
		"2026/08/22 17:45:33 INFO  : ",
		"Transferred:   \t          0 B / 0 B, -, 0 B/s, ETA -",
		"Errors:                 8 (fatal error encountered)",
		"Checks:              9354 / 9354, 100%, Listed 11370",
		"Elapsed time:      5m14.6s",
		"",
		// The next run, appended to the same file half an hour later.
		`2026/08/22 18:40:00 INFO  : Synching Path1 "/home/user/Documents/" with Path2 "gdrive:Documents/"`,
		"2026/08/22 18:40:47 INFO  : ",
		"Transferred:   \t          0 B / 0 B, -, 0 B/s, ETA -",
		"Checks:              9418 / 9418, 100%, Listed 11259",
		"Elapsed time:        47.6s",
		"",
	)

	if tail.job.Finished {
		t.Error("the new run is still going; the old one's ending is not its own")
	}
	if tail.job.Outcome != "" {
		t.Errorf("outcome = %q, want it cleared by the new run", tail.job.Outcome)
	}
	if len(tail.job.Errors) != 0 {
		t.Errorf("the previous run's errors are still attached: %+v", tail.job.Errors)
	}
	if tail.job.Stats.Errors != 0 || tail.job.Stats.Checks != 9418 {
		t.Errorf("stats came from the wrong run: %+v", tail.job.Stats)
	}
}

// Only bisync announces itself. A sync or a copy appended to the same log file
// starts with no marker at all, and the one signal that a new one has begun is
// its elapsed time, which counts up within a run and can only go backwards
// between two of them.
func TestElapsedGoingBackwardsStartsANewRun(t *testing.T) {
	tail := feed(
		"2026/08/22 21:04:28 ERROR : notes/todo.md: Failed to copy: failed to open source object: RootURL not set",
		"2026/08/22 21:04:31 INFO  : Signal received: terminated",
		"2026/08/22 21:04:31 NOTICE: ",
		"Transferred:   \t    4.932 GiB / 4.932 GiB, 100%, 12.408 MiB/s, ETA 0s",
		"Errors:                 6 (retrying may help)",
		"Checks:                 0 / 0, -, Listed 5576",
		"Transferred:         4667 / 4667, 100%",
		"Elapsed time:      6m47.0s",
		"",
		"2026/08/22 21:05:31 NOTICE: ",
		"Transferred:   \t  731.185 MiB / 4.932 GiB, 14%, 12.408 MiB/s, ETA 5m48s",
		"Checks:                 0 / 0, -, Listed 5576",
		"Transferred:          501 / 4667, 11%",
		"Elapsed time:         1m0.0s",
		"",
	)

	s := tail.job.Stats
	if s.Elapsed != time.Minute {
		t.Errorf("elapsed = %s, want the new run's 1m0s", s.Elapsed)
	}
	if s.Errors != 0 {
		t.Errorf("errors = %d, want the previous run's six forgotten", s.Errors)
	}
	if s.Transfers != 501 {
		t.Errorf("transfers = %d, want 501", s.Transfers)
	}
	// The run that was interrupted is over and gone; this is a different one,
	// and reporting it as interrupted would be describing the wrong job.
	if tail.job.Finished || tail.job.Outcome != "" {
		t.Errorf("outcome = %q (finished %v), want the new run to have neither",
			tail.job.Outcome, tail.job.Finished)
	}
	if len(tail.job.Errors) != 0 {
		t.Errorf("the interrupted run's errors are still attached: %+v", tail.job.Errors)
	}
}

// The line below is a real --use-json-log entry, with the paths neutralised
// and nothing else touched.
//
// It is the whole argument for preferring the JSON format: the text inside it
// says "643.062 KiB", which is 658495 bytes, while the stats object beside it
// says 658496. The text format cannot do better -- it rounds to three decimals
// before it is ever written -- so where both are present the object wins.
const jsonStatsLine = `{"time":"2026-08-23T15:25:58.436819465+02:00","level":"notice","msg":"\nTransferred:   \t  643.062 KiB / 2.899 MiB, 22%, 0 B/s, ETA -\nChecks:                 0 / 0, -, Listed 2\nTransferred:            1 / 2, 50%\nElapsed time:         0.3s\nTransferring:\n *                                       big.bin: 20% / 2.861 MiB, 0 B/s, -\n\n","stats":{"bytes":658496,"checks":0,"deletedDirs":0,"deletes":0,"elapsedTime":0.30048906,"errors":0,"eta":null,"fatalError":false,"listed":2,"renames":0,"retryError":false,"speed":0,"totalBytes":3040000,"totalChecks":0,"totalTransfers":2,"transferTime":0.300224808,"transferring":[{"bytes":618496,"dstFs":"gdrive:backup","eta":null,"group":"global_stats","name":"big.bin","percentage":20,"size":3000000,"speed":2065831.93,"speedAvg":0,"srcFs":"/home/user/src"}],"transfers":1},"source":"accounting/stats.go:551"}`

func TestJSONStatsArePreferredOverTheirOwnText(t *testing.T) {
	tail := feed(jsonStatsLine)

	if !tail.job.HaveStats {
		t.Fatal("the entry carries a stats object")
	}
	s := tail.job.Stats
	if s.Bytes != 658496 {
		t.Errorf("bytes = %d, want the exact 658496 rather than the text's 658495", s.Bytes)
	}
	if s.TotalBytes != 3040000 {
		t.Errorf("total bytes = %d, want 3040000", s.TotalBytes)
	}
	if s.Transfers != 1 || s.TotalTransfers != 2 {
		t.Errorf("transfers = %d/%d, want 1/2", s.Transfers, s.TotalTransfers)
	}
	if s.ETAKnown {
		t.Error("the ETA is null, which is not an estimate of zero")
	}
	if s.Elapsed < 300*time.Millisecond || s.Elapsed > 301*time.Millisecond {
		t.Errorf("elapsed = %s, want about 300ms", s.Elapsed)
	}
	// The JSON timestamp carries a zone, unlike the text one.
	want := time.Date(2026, 8, 23, 15, 25, 58, 436819465, time.FixedZone("", 2*60*60))
	if !tail.job.At.Equal(want) {
		t.Errorf("timestamp = %s, want %s", tail.job.At, want)
	}
}

// Not every JSON entry carries statistics: the object is attached to the stats
// message only. The rest still have to be read for what they say.
func TestJSONNarrativeAndErrors(t *testing.T) {
	tail := feed(
		`{"time":"2026-08-23T15:25:58.134662251+02:00","level":"info","msg":"Synching Path1 \"/home/user/Documents/\" with Path2 \"gdrive:Documents/\"","source":"bisync/bisync.go:159"}`,
		`{"time":"2026-08-23T15:25:58.168566867+02:00","level":"info","msg":"Copied (new)","size":40000,"object":"small.bin","objectType":"*local.Object","source":"operations/copy.go:380"}`,
		`{"time":"2026-08-23T15:25:59.168566867+02:00","level":"error","msg":"notes/todo.md: Failed to copy: failed to open source object: RootURL not set","source":"operations/copy.go:380"}`,
		`{"time":"2026-08-23T15:26:00.168566867+02:00","level":"info","msg":"Bisync successful","source":"bisync/bisync.go:400"}`,
	)

	if tail.job.Path1 != "/home/user/Documents/" || tail.job.Path2 != "gdrive:Documents/" {
		t.Errorf("paths = %q / %q", tail.job.Path1, tail.job.Path2)
	}
	if len(tail.job.Errors) != 1 {
		t.Fatalf("got %d errors, want 1: %+v", len(tail.job.Errors), tail.job.Errors)
	}
	if tail.job.Errors[0].Priority != 3 {
		t.Errorf("priority = %d, want 3: JSON levels are lower case", tail.job.Errors[0].Priority)
	}
	if !tail.job.Finished || tail.job.Outcome != "successful" {
		t.Errorf("outcome = %q (finished %v), want successful", tail.job.Outcome, tail.job.Finished)
	}
}

// A stats message with no object beside it still has its text, and that is
// better than reporting no progress at all.
func TestJSONWithoutAStatsObjectFallsBackToItsText(t *testing.T) {
	tail := feed(`{"time":"2026-08-23T15:25:58.436819465+02:00","level":"notice","msg":"\nTransferred:   \t  643.062 KiB / 2.899 MiB, 22%, 0 B/s, ETA -\nChecks:                 0 / 0, -, Listed 2\nElapsed time:         0.3s\n\n"}`)

	if !tail.job.HaveStats {
		t.Fatal("the text of the message describes a complete block")
	}
	if got := tail.job.Stats.Bytes; got != 658495 {
		t.Errorf("bytes = %d, want the text's own 658495", got)
	}
}

// A corrupt timestamp costs that one line's time, not the job's.
func TestAnUnparsableTimestampDoesNotEraseTheKnownOne(t *testing.T) {
	tail := feed(
		"2026/08/22 23:52:22 INFO  : Building Path1 and Path2 listings",
		"2026/13/45 99:99:99 ERROR : one.md: Failed to copy: RootURL not set",
	)

	want := time.Date(2026, 8, 22, 23, 52, 22, 0, time.Local)
	if !tail.job.At.Equal(want) {
		t.Errorf("timestamp = %s, want the last good one %s", tail.job.At, want)
	}
	if len(tail.job.Errors) != 1 || !tail.job.Errors[0].At.Equal(want) {
		t.Errorf("the error was dated %v, want the last good time", tail.job.Errors)
	}
}

// A line that is neither is not a reason to stop reading the file.
func TestGarbageLinesAreIgnored(t *testing.T) {
	tail := feed(
		`{"time":"nonsense","level":`,
		`{}`,
		`not a log line at all`,
		"2026/08/22 23:52:22 INFO  : Bisync successful",
	)

	if !tail.job.Finished {
		t.Error("the last line is good and should still have been read")
	}
}

// A block is only worth reporting once it is complete. Half of one, still being
// written as the tail reads it, must not overwrite the last full sample with a
// partial one.
func TestIncompleteStatsBlockIsNotCommitted(t *testing.T) {
	tail := feed(
		"2026/08/22 17:45:33 INFO  : ",
		"Transferred:   \t  731.185 MiB / 4.932 GiB, 14%, 12.408 MiB/s, ETA 5m48s",
		"Checks:              9354 / 9354, 100%, Listed 11370",
		"Elapsed time:      5m14.6s",
		"",
		"2026/08/22 17:46:33 INFO  : ",
		"Transferred:   \t    1.431 GiB / 4.932 GiB, 29%, 12.257 MiB/s, ETA 4m52s",
	)

	if got := tail.job.Stats.Bytes; got != 766703042 {
		t.Errorf("bytes = %d, want the last complete block's 766703042", got)
	}
}
