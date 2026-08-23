package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// plain renders a model's unit section with the styling stripped, so the tests
// assert on what is written rather than on escape sequences.
func plain(m Model, width int) string {
	out := m.denseUnits(m.state.Resolve().Units, width)
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		b.WriteString(stripANSI(line))
		b.WriteString("\n")
	}
	return b.String()
}

func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K'):
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func modelWith(units []model.Unit, now time.Time) Model {
	lipgloss.SetColorProfile(0) // Ascii: no escape sequences at all
	m := New(nil, Options{}, nil)
	m.now = now
	m.state.Units = units
	return m
}

func TestUnitSectionFoldsTimerIntoService(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{
		{
			Name: "jd-bisync.service", Scope: "user",
			ActiveState: "inactive", SubState: "dead", Result: "success",
			InactiveEnter: now.Add(-2 * time.Minute),
		},
		{
			Name: "jd-bisync.timer", Scope: "user",
			ActiveState: "active", SubState: "waiting",
			Triggers: "jd-bisync.service", NextElapse: now.Add(28 * time.Minute),
		},
	}, now)

	got := plain(m, 80)

	// One line for the pair, not two: a timer's schedule is only meaningful
	// beside the result of the job it starts.
	if strings.Count(got, "UNIT") != 1 {
		t.Errorf("expected a single unit block, got:\n%s", got)
	}
	if strings.Contains(got, ".timer") {
		t.Errorf("the timer was listed separately:\n%s", got)
	}
	for _, want := range []string{"jd-bisync", "last 2m0s ago", "next in 28m0s"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestFinishedOneshotIsNotCalledIdleWhenItFailed(t *testing.T) {
	// systemd leaves a finished oneshot "inactive" whether it worked or not,
	// so the active state alone cannot answer "did it work". These are the real
	// properties of a job killed with SIGTERM: ExecMainCode=2 is CLD_KILLED and
	// ExecMainStatus is then the signal, not an exit code.
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "jd-bisync.service", Scope: "user",
		ActiveState: "inactive", SubState: "dead",
		Result: "signal", ExitCode: "2", ExitStatus: 15,
		InactiveEnter: now.Add(-time.Hour),
	}}, now)

	got := plain(m, 80)
	if !strings.Contains(got, "signal") {
		t.Errorf("the failure result is not shown:\n%s", got)
	}
	if !strings.Contains(got, "killed by SIGTERM") {
		t.Errorf("the signal is not reported:\n%s", got)
	}
	if strings.Contains(got, "exit 15") {
		t.Errorf("a signal number was rendered as an exit code:\n%s", got)
	}
}

// TestKilledUnitIsNotReportedAsExiting is a regression test.
//
// A unit stopped normally reports Result=success with ExecMainCode=2 and
// ExecMainStatus=15. Reading that status as an exit code printed "exit 15" in
// the alarm colour for a unit systemd calls successful: the wrong quantity and
// a false alarm at once.
func TestKilledUnitIsNotReportedAsExiting(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "helper.service", Scope: "system",
		ActiveState: "inactive", SubState: "dead",
		Result: "success", ExitCode: "2", ExitStatus: 15,
		InactiveEnter: now.Add(-time.Hour),
	}}, now)

	got := plain(m, 80)
	if strings.Contains(got, "exit 15") {
		t.Errorf("a signal number was rendered as an exit code:\n%s", got)
	}
	if !strings.Contains(got, "SIGTERM") {
		t.Errorf("the signal is not named:\n%s", got)
	}
}

// TestOneshotInFlightIsCalledRunning covers the state a scheduled backup is
// actually in while it works. systemd holds a Type=oneshot unit at
// "activating" for the whole of its ExecStart, so it is never "active" and
// would otherwise read as merely "starting" beside a stale last-run time.
func TestOneshotInFlightIsCalledRunning(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "jd-bisync.service", Scope: "user",
		ActiveState: "activating", SubState: "start", Result: "success",
		ActiveEnter: now.Add(-90 * time.Second),
	}}, now)

	got := plain(m, 80)
	if !strings.Contains(got, "running") {
		t.Errorf("a oneshot in flight is not called running:\n%s", got)
	}
	if !strings.Contains(got, "running for 1m30s") {
		t.Errorf("expected how long it has been going:\n%s", got)
	}
}

// TestRemainAfterExitIsNotIdle covers a oneshot held active after finishing.
// systemd counts active/exited as active; calling it "idle" makes it
// indistinguishable from one that never ran.
func TestRemainAfterExitIsNotIdle(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "rclone-setup.service", Scope: "system",
		ActiveState: "active", SubState: "exited", Result: "success",
		ActiveEnter: now.Add(-time.Hour),
	}}, now)

	if got := plain(m, 80); strings.Contains(got, "idle") {
		t.Errorf("active/exited was reported as idle:\n%s", got)
	}
}

// TestActiveUnitPrefersActiveEnter is a regression test.
//
// InactiveEnterTimestamp records the last transition into inactive, which for
// an active unit is a leftover from an earlier cycle and can be newer than the
// run in progress. Preferring it unconditionally reported a unit that started
// two hours ago as having last run twenty minutes ago.
func TestActiveUnitPrefersActiveEnter(t *testing.T) {
	now := time.Unix(1787433722, 0)
	started := now.Add(-2 * time.Hour)
	m := modelWith([]model.Unit{{
		Name: "kmod.service", Scope: "system",
		ActiveState: "active", SubState: "exited", Result: "success",
		ActiveEnter:   started,
		InactiveEnter: now.Add(-20 * time.Minute),
	}}, now)

	if got := plain(m, 80); !strings.Contains(got, "last 2h0m ago") {
		t.Errorf("a stale inactive timestamp won over the current run:\n%s", got)
	}
}

// TestDueTimerIsNotUnknown covers the routine case of an elapse that has just
// passed. Duration answers "-" for a non-positive interval, and in this
// codebase that dash means "unknown", which a due timer is not.
func TestDueTimerIsNotUnknown(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{
		{Name: "jd-bisync.service", Scope: "user", ActiveState: "inactive", SubState: "dead", Result: "success"},
		{Name: "jd-bisync.timer", Scope: "user", ActiveState: "active",
			Triggers: "jd-bisync.service", NextElapse: now.Add(-3 * time.Second)},
	}, now)

	got := plain(m, 80)
	if strings.Contains(got, "next -") {
		t.Errorf("a due timer rendered as unknown:\n%s", got)
	}
	if !strings.Contains(got, "due now") {
		t.Errorf("expected a due timer to say so:\n%s", got)
	}
}

func TestRunningServiceReportsUptimeNotLastRun(t *testing.T) {
	// "last 14h ago" on a service that is still up reads as though it had
	// finished fourteen hours ago.
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "rclone-mount.service", Scope: "user",
		ActiveState: "active", SubState: "running", Result: "success",
		ActiveEnter: now.Add(-14 * time.Hour),
	}}, now)

	got := plain(m, 80)
	if !strings.Contains(got, "running for 14h0m") {
		t.Errorf("expected how long it has been up, got:\n%s", got)
	}
	if strings.Contains(got, "last ") {
		t.Errorf("a running service should not report a last run:\n%s", got)
	}
}

func TestStoppedTimerIsCalledOut(t *testing.T) {
	// A timer with no next elapse has been stopped, which is exactly what a
	// failing job can do to its own schedule. Rendering nothing there would
	// read as "no schedule configured".
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{
		{Name: "jd-bisync.service", Scope: "user", ActiveState: "inactive", SubState: "dead", Result: "success"},
		{Name: "jd-bisync.timer", Scope: "user", ActiveState: "inactive", Triggers: "jd-bisync.service"},
	}, now)

	if got := plain(m, 80); !strings.Contains(got, "timer stopped") {
		t.Errorf("a stopped timer was rendered as a blank:\n%s", got)
	}
}

func TestFailuresSortFirst(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{
		{Name: "a-healthy.service", ActiveState: "active", SubState: "running", Result: "success"},
		{Name: "z-broken.service", ActiveState: "failed", Result: "exit-code", ExitStatus: 1},
	}, now)

	got := plain(m, 80)
	if strings.Index(got, "z-broken") > strings.Index(got, "a-healthy") {
		t.Errorf("a failing unit sorted below a healthy one:\n%s", got)
	}
}

func TestJournalErrorIsFlattenedAndTrimmed(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "rclone-mount.service", ActiveState: "active", SubState: "running", Result: "success",
		ActiveEnter: now.Add(-time.Hour),
		Errors: []model.LogLine{
			{At: now.Add(-2 * time.Hour), Priority: 3, Message: "older"},
			{At: now.Add(-time.Minute), Priority: 3, Message: "vfs cache:\n\tfailed to write\n"},
		},
	}}, now)

	got := plain(m, 80)
	// The newest is the one shown, flattened onto one line.
	if !strings.Contains(got, "vfs cache: failed to write") {
		t.Errorf("the newest error was not shown flattened:\n%s", got)
	}
	if strings.Contains(got, "older") {
		t.Errorf("an older error crowded the line:\n%s", got)
	}
	if !strings.Contains(got, "and 1 more recent") {
		t.Errorf("the count of further errors is missing:\n%s", got)
	}

	for _, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Errorf("line overflows the terminal (%d): %q", lipgloss.Width(line), line)
		}
	}
}

func TestNoUnitsRendersNothing(t *testing.T) {
	m := modelWith(nil, time.Unix(1787433722, 0))
	if rows := m.state.Resolve().Units; len(rows) != 0 {
		t.Fatalf("got %d rows out of no units", len(rows))
	}
	if got := m.denseUnits(nil, 80); got != "" {
		t.Errorf("got %q, want an empty section", got)
	}
}

// TestNarrowTerminalDoesNotOverflow is a regression test.
//
// Truncate used to return the whole string when asked for a non-positive
// width, so every arithmetic slip in a caller's budget became an overflowing
// line -- and only on a narrow terminal, where it is least likely to be
// noticed. A journal message rendered at a computed width of -5 came out at a
// hundred and sixty columns.
func TestNarrowTerminalDoesNotOverflow(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{{
		Name: "a-very-long-unit-name-that-will-not-fit.service", Scope: "system",
		ActiveState: "inactive", SubState: "dead", Result: "exec-condition",
		InactiveEnter: now.Add(-time.Hour),
		Errors: []model.LogLine{{
			At: now.Add(-time.Minute), Priority: 3,
			Message: strings.Repeat("a long journal message that keeps going ", 10),
		}},
	}}, now)

	// Rendered through the whole view, because that is where the final clamp
	// lives: some content genuinely cannot fit a very narrow terminal, and the
	// guarantee is that it is cut rather than wrapped.
	for _, width := range []int{10, 15, 24, 27, 40, 60, 80, 120} {
		m.width = width
		for _, line := range strings.Split(stripANSI(m.renderDense()), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Errorf("at width %d a line came out %d wide: %q", width, got, line)
			}
		}
	}
}

func TestTruncateYieldsNothingWithoutRoom(t *testing.T) {
	for _, width := range []int{0, -1, -100} {
		if got := Truncate("something", width, false); got != "" {
			t.Errorf("Truncate(width %d) = %q, want empty", width, got)
		}
	}
}

// TestUnitOfAShownProcessIsNotRepeated covers the redundancy between the two
// sections. A mount appears as a process line with its throughput, and its
// systemd unit would describe the same thing again in different words -- "up
// 14h40m" against "running for 14h40m". The process line wins; what only the
// unit knows travels with it.
func TestUnitOfAShownProcessIsNotRepeated(t *testing.T) {
	now := time.Unix(1787433722, 0)
	m := modelWith([]model.Unit{
		{
			Name: "rclone-mount.service", Scope: "user",
			ActiveState: "active", SubState: "running", Result: "success",
			ActiveEnter: now.Add(-14 * time.Hour),
			Errors: []model.LogLine{{
				At: now.Add(-time.Minute), Priority: 3, Message: "vfs cache: RootURL not set",
			}},
		},
		{
			Name: "jd-bisync.service", Scope: "user",
			ActiveState: "inactive", SubState: "dead", Result: "success",
			InactiveEnter: now.Add(-2 * time.Minute),
		},
	}, now)
	m.state.Processes = []model.Process{{
		PID: 2702, Kind: model.KindMount, Unit: "rclone-mount.service",
		Remotes: []string{"gdrive:"}, Paths: []string{"gdrive:", "/mnt"},
		StartedAt: now.Add(-14 * time.Hour), IOAvailable: true,
	}}
	// Without a collector on record the view reports "collecting…" instead of
	// what it has, which is the honest thing everywhere but here.
	m.state.Seen[model.SourceProc] = now
	m.width = 100

	got := stripANSI(m.renderDense())

	if strings.Contains(got, "UNIT   rclone-mount") {
		t.Errorf("the mount is described twice:\n%s", got)
	}
	// The job with no process of its own keeps its line.
	if !strings.Contains(got, "jd-bisync") {
		t.Errorf("a unit with no process lost its line:\n%s", got)
	}
	// And the unit's journal error is not lost with the suppressed line.
	if !strings.Contains(got, "RootURL not set") {
		t.Errorf("the unit's error was dropped:\n%s", got)
	}
	if strings.Count(got, "RootURL not set") != 1 {
		t.Errorf("the error is shown more than once:\n%s", got)
	}
}

// TestErrorColourCoolsWithAge covers the difference between "something is
// wrong" and "something went wrong hours ago". The age is printed either way,
// but painting a five-hour-old failure as brightly as a fresh one says
// something untrue about how urgent it is.
func TestErrorColourCoolsWithAge(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(0)

	now := time.Unix(1787433722, 0)
	m := modelWith(nil, now)

	fresh := m.fadedAlarm(now.Add(-time.Minute)).Render("x")
	old := m.fadedAlarm(now.Add(-5 * time.Hour)).Render("x")
	ancient := m.fadedAlarm(now.Add(-48 * time.Hour)).Render("x")

	if fresh == old {
		t.Errorf("a five-hour-old error is painted like a fresh one: %q", fresh)
	}
	if old == ancient {
		t.Errorf("the fade does not continue past five hours: %q vs %q", old, ancient)
	}
	// Past the window it settles on the inactive colour rather than drifting on.
	settled := m.fadedAlarm(now.Add(-100 * time.Hour)).Render("x")
	if settled != ancient {
		t.Errorf("the fade did not clamp: %q vs %q", settled, ancient)
	}
	// And a fresh one is the alarm colour itself.
	if want := lipgloss.NewStyle().
		Foreground(m.opts.Theme.Color("hi_fg").Lipgloss()).Render("x"); fresh != want {
		t.Errorf("a fresh error is not the alarm colour: %q, want %q", fresh, want)
	}
}
