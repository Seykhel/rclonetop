package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// plainProcess renders one process block with the styling stripped.
//
// It goes through Resolve rather than fabricating a row, so these tests keep
// asserting on what the process line says once the job and the journal have
// been folded into it -- which is the thing that used to be done inline here.
func plainProcess(m Model, p model.Process, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(m.denseProcess(rowFor(m, p.PID), width), "\n") {
		b.WriteString(stripStyles(line))
		b.WriteString("\n")
	}
	return b.String()
}

// rowFor is the resolved row of one process, by PID.
func rowFor(m Model, pid int) model.ProcRow {
	for _, row := range m.state.Resolve().Procs {
		if row.Process.PID == pid {
			return row
		}
	}
	return model.ProcRow{}
}

func modelWithJobs(procs []model.Process, jobs []model.Job, now time.Time) Model {
	lipgloss.SetColorProfile(0) // Ascii: no escape sequences at all
	m := New(nil, Options{}, nil)
	m.now = now
	m.state.Processes = procs
	m.state.Jobs = jobs
	return m
}

// A running bisync out of the fixtures: 2.87 GiB of 4.93 GiB moved, 1158 files
// of 4667 done. None of that is knowable from /proc, which can only say how
// many bytes went past.
func TestProgressLineShowsHowFarAlongTheRunIs(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile:   "/var/log/rclone.log",
		PID:       193345,
		HaveStats: true,
		Stats: model.JobStats{
			Bytes: 3080000000, TotalBytes: 5295694675,
			Transfers: 1158, TotalTransfers: 4667,
			ETA: 2*time.Minute + 51*time.Second, ETAKnown: true,
		},
	}}, now)

	got := plainProcess(m, proc, 80)

	for _, want := range []string{"58%", "2.9 GiB / 4.9 GiB", "1158/4667 files", "ETA 2m51s"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRCProgressIsShownAlongsideTheProcess(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, RCAddr: "127.0.0.1:5572", IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, nil, now)
	m.state.RCStats = []model.RCStats{{
		Addr:  proc.RCAddr,
		Stats: model.JobStats{Bytes: 100, TotalBytes: 200, Transfers: 1, TotalTransfers: 2, Speed: 12.5, Elapsed: 3 * time.Second},
	}}

	got := plainProcess(m, proc, 80)
	for _, want := range []string{"RC ", "50%", "100 B / 200 B", "1/2 files", "speed 12 B/s", "elapsed 3s"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestAsyncRCJobsShowKnownOutcomesWithoutInventingOne(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, RCAddr: "127.0.0.1:5572", IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, nil, now)
	m.state.RCStats = []model.RCStats{{Addr: proc.RCAddr, Jobs: []model.RCJob{
		{ID: 1},
		{ID: 2, Finished: true, Success: true, SuccessKnown: true},
		{ID: 3, Finished: true, SuccessKnown: true, Error: "remote failed"},
		{ID: 4, Finished: true},
	}}}

	got := plainProcess(m, proc, 100)
	for _, want := range []string{"job #1 running", "job #2 successful", "job #3 failed", "remote failed", "job #4 finished"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "job #4 failed") {
		t.Errorf("missing success field was rendered as failure:\n%s", got)
	}
}

// Until the first statistics block lands there is nothing to say, and a bar at
// nought per cent would be a claim about progress rather than a report of it.
func TestProgressLineIsSuppressedWithoutStatistics(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345,
	}}, now)

	if got := plainProcess(m, proc, 80); strings.Contains(got, "%") {
		t.Errorf("a progress line appeared with no statistics behind it:\n%s", got)
	}
}

// Two rclone processes on one host each have their own log. Showing one's
// progress against the other's identity would be worse than showing none.
func TestProgressLineBelongsToItsOwnProcess(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/other.log", PID: 999, HaveStats: true,
		Stats: model.JobStats{Bytes: 3080000000, TotalBytes: 5295694675},
	}}, now)

	if got := plainProcess(m, proc, 80); strings.Contains(got, "58%") {
		t.Errorf("another process's progress was drawn here:\n%s", got)
	}
}

func TestUnknownETAIsNotShownAsZero(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345, HaveStats: true,
		Stats: model.JobStats{Bytes: 100, TotalBytes: 200},
	}}, now)

	if got := plainProcess(m, proc, 80); strings.Contains(got, "ETA") {
		t.Errorf("an unknown ETA was rendered anyway:\n%s", got)
	}
}

// The common case for a healthy bisync: nothing to move, thousands of files
// checked. With only the transfer counters on the line it would look idle.
func TestARunWithNothingToTransferStillReportsItsChecks(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345, HaveStats: true,
		Stats: model.JobStats{Checks: 9418, TotalChecks: 9418, Elapsed: 47600 * time.Millisecond},
	}}, now)

	got := plainProcess(m, proc, 80)
	if !strings.Contains(got, "9418 checked") {
		t.Errorf("the checks are missing from:\n%s", got)
	}
	if strings.Contains(got, "%") {
		t.Errorf("a percentage of nothing is not a measurement:\n%s", got)
	}
}

func TestErrorCountIsShown(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345, HaveStats: true,
		Stats: model.JobStats{Checks: 9354, TotalChecks: 9354, Errors: 8, FatalError: true},
	}}, now)

	if got := plainProcess(m, proc, 80); !strings.Contains(got, "8 errors") {
		t.Errorf("the error count is missing from:\n%s", got)
	}
}

// The log is the only place a job's own errors appear when it writes to a file
// rather than to the journal.
func TestLogErrorsAppearUnderTheProcess(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345,
		Errors: []model.LogLine{{
			At:       now.Add(-90 * time.Second),
			Priority: 3,
			Message:  "notes/todo.md: Failed to copy: failed to open source object: RootURL not set",
		}},
	}}, now)

	// Truncated to the width, like every other error line: the point is that
	// something failed and roughly when, not to be a log viewer.
	got := plainProcess(m, proc, 80)
	if !strings.Contains(got, "notes/todo.md: Failed to copy") {
		t.Errorf("the log error is missing from:\n%s", got)
	}
	if !strings.Contains(got, "1m30s ago") {
		t.Errorf("the error's age is missing from:\n%s", got)
	}
}

// A job whose log cannot be read stands as still as one with nothing to do.
// Saying which is the same distinction the throughput line already makes when
// /proc/<pid>/io belongs to someone else.
func TestAnUnreadableLogSaysSo(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindBisync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile:   "/var/log/rclone.log",
		PID:       193345,
		ReadError: "open /var/log/rclone.log: permission denied",
	}}, now)

	got := plainProcess(m, proc, 100)
	if !strings.Contains(got, "log unreadable") {
		t.Errorf("nothing explains the silence:\n%s", got)
	}
}

// The whole point of recovering the paths: "home_user_Documents" is what is on
// disk, but it is not what anyone typed.
func TestSyncPairPrefersTheRealPaths(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWithJobs(nil, nil, now)
	m.state.SyncPairs = []model.SyncPair{{
		Name:  "home_user_Documents..gdrive_Documents",
		Left:  model.SyncSide{Label: "home_user_Documents", Path: "/home/user/Documents/", Files: 10},
		Right: model.SyncSide{Label: "gdrive_Documents", Path: "gdrive:Documents/", Files: 10},
	}}

	got := stripStyles(m.denseSyncPair(m.state.SyncPairs[0], 80))
	if !strings.Contains(got, "gdrive:Documents") {
		t.Errorf("the real path is missing from:\n%s", got)
	}
	if strings.Contains(got, "gdrive_Documents") {
		t.Errorf("the mangled label is still being shown:\n%s", got)
	}
}

// The same guarantee the unit section already carries, extended to the two
// things the log collector adds: a progress line and a pair of real paths,
// either of which can be longer than the terminal.
func TestJobContentDoesNotOverflow(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{
		PID: 193345, Kind: model.KindBisync, IOAvailable: true,
		Paths: []string{"/home/user/a/very/long/local/path/that/keeps/going", "gdrive:and/a/remote/one/just/as/long"},
	}
	m := modelWithJobs([]model.Process{proc}, []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345, HaveStats: true,
		Stats: model.JobStats{
			Bytes: 3080000000, TotalBytes: 5295694675,
			Transfers: 1158, TotalTransfers: 4667,
			Checks: 9354, TotalChecks: 9354, Errors: 8, FatalError: true,
			ETA: 2*time.Minute + 51*time.Second, ETAKnown: true,
		},
		Errors: []model.LogLine{{
			At: now.Add(-time.Minute), Priority: 3,
			Message: strings.Repeat("a long log message that keeps going ", 10),
		}},
		Transferring: []model.Transfer{{
			Name:       strings.Repeat("a/long/directory/name/", 8) + "and-a-file.bin",
			Percentage: 82, Size: 30 << 20, Speed: 1 << 22,
			ETA: 22 * time.Minute, ETAKnown: true,
		}},
	}}, now)
	m.state.SyncPairs = []model.SyncPair{{
		Name:  "home_user_Documents..gdrive_Documents",
		Left:  model.SyncSide{Label: "home_user_Documents", Path: "/home/user/a/very/long/local/path/that/keeps/going", Files: 10},
		Right: model.SyncSide{Label: "gdrive_Documents", Path: "gdrive:and/a/remote/one/just/as/long", Files: 10},
	}}

	for _, width := range []int{10, 15, 24, 27, 40, 60, 80, 120} {
		m.width = width
		for _, line := range strings.Split(stripStyles(m.renderDense()), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("at width %d a line came out %d wide: %q", width, got, line)
			}
		}
	}
}

// A session no log has described still has to be shown, under the only name
// there is for it.
func TestSyncPairFallsBackToTheMangledLabel(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWithJobs(nil, nil, now)
	pair := model.SyncPair{
		Name:  "home_user_Documents..gdrive_Documents",
		Left:  model.SyncSide{Label: "home_user_Documents", Files: 10},
		Right: model.SyncSide{Label: "gdrive_Documents", Files: 10},
	}

	if got := stripStyles(m.denseSyncPair(pair, 80)); !strings.Contains(got, "gdrive_Documents") {
		t.Errorf("nothing identifies the session in:\n%s", got)
	}
}

// modelWithUnitLog builds a unit whose log the systemd collector found, and the
// job the log collector then read from it.
func modelWithUnitLog(outcome string, errs []model.LogLine, now time.Time) Model {
	lipgloss.SetColorProfile(0)
	m := New(nil, Options{}, nil)
	m.now = now
	m.state.Units = []model.Unit{{
		Name: "jd-bisync.service", Scope: "user",
		ActiveState: "inactive", SubState: "dead", Result: "success",
		InactiveEnter: now.Add(-2 * time.Minute),
		LogFile:       "/home/user/.local/state/jd-backup/bisync.log",
	}}
	m.state.Jobs = []model.Job{{
		LogFile:  "/home/user/.local/state/jd-backup/bisync.log",
		Outcome:  outcome,
		Finished: outcome != "",
		Errors:   errs,
	}}
	return m
}

// A job started with --log-file writes nothing to the journal, so between runs
// the unit line has systemd's verdict and nothing else. The log has the words.
func TestTheUnitLineShowsWhatItsLogSaid(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWithUnitLog("aborted", []model.LogLine{{
		At:       now.Add(-3 * time.Minute),
		Priority: 3,
		Message:  "Bisync critical error: failed to set directory modtime",
	}}, now)

	got := plain(m, 100)
	if !strings.Contains(got, "aborted") {
		t.Errorf("the outcome the log recorded is missing from:\n%s", got)
	}
	if !strings.Contains(got, "Bisync critical error") {
		t.Errorf("the log's own error is missing from:\n%s", got)
	}
}

// A unit whose log said nothing in particular is left exactly as it was.
func TestAUnitWithoutALogIsUnchanged(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWithUnitLog("", nil, now)

	if got := plain(m, 100); strings.Contains(got, "!") {
		t.Errorf("something was invented for a log with nothing to say:\n%s", got)
	}
}

// inFlight is a job with files under way, ready to hang off pid 193345.
func inFlight(ts ...model.Transfer) []model.Job {
	return []model.Job{{
		LogFile: "/var/log/rclone.log", PID: 193345, HaveStats: true,
		Stats:        model.JobStats{Bytes: 1 << 20, TotalBytes: 4 << 20, Transfers: 1, TotalTransfers: 6},
		Transferring: ts,
	}}
}

// The rows the dense view otherwise does not have: what rclone is moving right
// now, rather than only how much of it. Nothing but the log can say this.
func TestFilesInFlightAreNamedUnderTheirProcess(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, inFlight(
		model.Transfer{
			Name: "30-39 Reference/31 Papers/notes on geology.pdf", Percentage: 78,
			Size: 128 << 20, Speed: 3 << 20, ETA: 7 * time.Second, ETAKnown: true,
		},
		model.Transfer{Name: "big.bin", Percentage: 20, Size: 3 << 20},
	), now)

	got := plainProcess(m, proc, 100)

	for _, want := range []string{
		"31 Papers/notes on geology.pdf", "78% of 128 MiB", "3.0 MiB/s", "ETA 7s",
		"big.bin", "20% of 3.0 MiB",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// rclone writes "-" for a file it cannot estimate, and a run of small files
// spends most of its time in exactly that state. Zero would read as "any
// moment now", which is the opposite.
func TestAFileWithNoEstimateIsNotGivenOne(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc},
		inFlight(model.Transfer{Name: "big.bin", Percentage: 20, Size: 3 << 20}), now)

	if got := plainProcess(m, proc, 100); strings.Contains(got, "ETA") {
		t.Errorf("an estimate appeared where rclone made none:\n%s", got)
	}
}

// A size of -1 is rclone saying it could not learn how big the source was. Zero
// is a real answer -- an empty file -- so it cannot stand in for that one, and
// a bare percentage would be a fraction of nothing.
func TestAFileOfUnknownSizeIsNotCalledEmpty(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc},
		inFlight(model.Transfer{Name: "stream.bin", Percentage: 0, Size: -1}), now)

	got := plainProcess(m, proc, 100)
	if !strings.Contains(got, "unknown size") {
		t.Errorf("missing the unknown size in:\n%s", got)
	}
	if strings.Contains(got, "of 0 B") {
		t.Errorf("an unmeasured size was rendered as an empty file:\n%s", got)
	}
}

// A run with --transfers 16 would otherwise push everything below it off the
// screen. What is left out is counted: four rows and no count read as a job
// with four files left in it.
func TestALongFileListIsCappedAndSaysWhatItLeftOut(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}

	var ts []model.Transfer
	for i := 0; i < 7; i++ {
		ts = append(ts, model.Transfer{
			Name: fmt.Sprintf("file-%d.bin", i), Percentage: 50, Size: 1 << 20,
		})
	}
	m := modelWithJobs([]model.Process{proc}, inFlight(ts...), now)

	got := plainProcess(m, proc, 100)
	if n := strings.Count(got, "↳ "); n != maxInFlight {
		t.Errorf("got %d rows, want %d:\n%s", n, maxInFlight, got)
	}
	if !strings.Contains(got, "and 3 more transferring") {
		t.Errorf("the three left out went unmentioned:\n%s", got)
	}
}

// Both silences draw nothing, and there is no third way of spelling an empty
// screen: the progress line above has already accounted for the run.
func TestNoFilesInFlightDrawsNoRow(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}

	for name, ts := range map[string][]model.Transfer{
		"nothing has said":  nil,
		"nothing is moving": {},
	} {
		m := modelWithJobs([]model.Process{proc}, inFlight(ts...), now)
		if got := plainProcess(m, proc, 100); strings.Contains(got, "↳") {
			t.Errorf("%s, and yet a row was drawn:\n%s", name, got)
		}
	}
}

// Below a certain width the name is all that would be cut, and a percentage
// with no name beside it says that something is moving without saying what --
// which the progress line above has already said better.
func TestATerminalTooNarrowForNamesDropsTheFileList(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc},
		inFlight(model.Transfer{Name: "some/long/path/big.bin", Percentage: 20, Size: 3 << 20}), now)

	if got := plainProcess(m, proc, 30); strings.Contains(got, "of 3.0 MiB") {
		t.Errorf("the row survived a terminal with no room for its name:\n%s", got)
	}
	if got := plainProcess(m, proc, 100); !strings.Contains(got, "of 3.0 MiB") {
		t.Errorf("and it should still be there when there is room:\n%s", got)
	}
}

// Four rows whose figures each start wherever their name happened to end read
// as four unrelated statements. One column for all of them is most of what
// makes a list legible at a glance, which is the gap with btop this is closing.
func TestTheFiguresLineUpInOneColumn(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, inFlight(
		model.Transfer{Name: "a/very/long/name/that/goes/on/big.bin", Percentage: 82,
			Size: 30 << 20, Speed: 4200, ETA: 22 * time.Minute, ETAKnown: true},
		model.Transfer{Name: "short.bin", Percentage: 7, Size: 3 << 20, Speed: 1200},
	), now)

	var at []int
	for _, line := range strings.Split(plainProcess(m, proc, 100), "\n") {
		for _, figure := range []string{"82% of", "7% of"} {
			if i := strings.Index(line, figure); i > 0 {
				at = append(at, i)
			}
		}
	}
	if len(at) != 2 {
		t.Fatalf("found %d figure columns, want 2", len(at))
	}
	if at[0] != at[1] {
		t.Errorf("figures start at columns %d and %d, want one column", at[0], at[1])
	}
}

// One file with a long estimate would otherwise set the shared width for all of
// them and take the whole list off the screen. What is in flight is worth more
// than when it will land, so the estimate goes first.
func TestANarrowTerminalShedsTheEstimateBeforeTheRows(t *testing.T) {
	now := time.Unix(1787433722, 0)
	proc := model.Process{PID: 193345, Kind: model.KindSync, IOAvailable: true}
	m := modelWithJobs([]model.Process{proc}, inFlight(
		model.Transfer{Name: "big.bin", Percentage: 82, Size: 30 << 20,
			Speed: 4200, ETA: 22*time.Minute + 8*time.Second, ETAKnown: true},
	), now)

	if got := plainProcess(m, proc, 80); !strings.Contains(got, "ETA 22m8s") {
		t.Errorf("the estimate should survive a terminal with room for it:\n%s", got)
	}

	got := plainProcess(m, proc, 45)
	if !strings.Contains(got, "82% of 30 MiB") {
		t.Errorf("the row went instead of the estimate:\n%s", got)
	}
	if strings.Contains(got, "ETA") {
		t.Errorf("the estimate survived a terminal with no room for it:\n%s", got)
	}
}
