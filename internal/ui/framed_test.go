package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
)

// busyModel is a host with something in every panel: two processes, a job with
// files in flight, a unit, a bisync pair and a cache. Rendering an empty state
// would let a panel that never draws anything pass a width test.
func busyModel(now time.Time) Model {
	lipgloss.SetColorProfile(0) // Ascii: no escape sequences at all
	m := New(nil, Options{}, nil)
	m.now = now
	m.width, m.height = 120, 40
	m.state.Processes = []model.Process{
		{
			PID: 193345, Kind: model.KindBisync, IOAvailable: true,
			Paths:     []string{"/home/seykhel/Documents"},
			Remotes:   []string{"icloud:Documents"},
			StartedAt: now.Add(-9 * time.Minute), RSS: 412 << 20, Threads: 14,
			ReadRate: 4 << 20, WriteRate: 11 << 20,
			ReadTotal: 3 << 30, WriteTotal: 9 << 30,
		},
		{
			PID: 900, Kind: model.KindMount, IOAvailable: true,
			Remotes: []string{"gdrive:"}, Paths: []string{"/home/seykhel/gdrive"},
			StartedAt: now.Add(-72 * time.Hour), RSS: 96 << 20, Threads: 8,
		},
	}
	m.state.Jobs = []model.Job{{
		LogFile: "/home/seykhel/.local/state/rclone/bisync.log", PID: 193345,
		HaveStats: true,
		Stats: model.JobStats{
			Bytes: 3080000000, TotalBytes: 5295694675,
			Transfers: 1158, TotalTransfers: 4667,
			ETA: 2*time.Minute + 51*time.Second, ETAKnown: true,
		},
		Transferring: []model.Transfer{
			{Name: "30-39 Reference/31 Papers/notes on geology.pdf", Bytes: 134 << 20, Size: 172 << 20, Percentage: 78, Speed: 3.686 * (1 << 20), ETA: 7 * time.Second},
			{Name: "10-19 Admin/11 Invoices/2026-08 invoice.pdf", Bytes: 2 << 20, Size: 9 << 20, Percentage: 22},
		},
	}}
	m.state.Units = []model.Unit{{
		Name: "jd-bisync.service", Scope: "user",
		ActiveState: "inactive", SubState: "dead", Result: "success",
		InactiveEnter: now.Add(-2 * time.Minute),
	}}
	m.state.Caches = []model.CacheDir{{Kind: "vfs", Bytes: 2 << 30, Files: 812, ScannedAt: now.Add(-30 * time.Second)}}
	m.state.Seen = map[model.Source]time.Time{model.SourceProc: now, model.SourceLog: now}
	return m
}

// The property the dense view already had and paid for once (2707cc4): whatever
// the arithmetic inside does, nothing reaches the terminal wider or taller than
// the terminal is. A line that wraps corrupts the layout of every line below it,
// and in a framed view it corrupts the frame itself.
func TestTheFramedViewNeverOutgrowsItsTerminal(t *testing.T) {
	m := busyModel(time.Unix(1787433722, 0))

	for _, size := range [][2]int{
		{60, 20}, {80, 24}, {99, 25}, {100, 30}, {120, 40}, {190, 60},
		// Below the threshold this falls through to the dense view,
		// which has to obey the same rule. Ten and fifteen are in the
		// sweep #11 names, and they are where a fixed label is wider
		// than the whole terminal.
		{40, 20}, {24, 10}, {15, 20}, {10, 20}, {0, 0},
	} {
		m.width, m.height = size[0], size[1]
		lines := strings.Split(m.renderFramed(), "\n")

		// The height is the framed view's own promise, and only its
		// own. The dense view it falls back to has never budgeted
		// against the height -- it says what it has to say and the
		// terminal scrolls -- so asserting it here would be asserting
		// something about a view this slice does not touch.
		if got, want := len(lines), effectiveHeight(size[1]); !planLayout(size[0], size[1]).dense && got > want {
			t.Errorf("%dx%d: %d lines, the terminal has %d", size[0], size[1], got, want)
		}
		for i, line := range lines {
			if got, want := lipgloss.Width(line), effectiveWidth(size[0]); got > want {
				t.Errorf("%dx%d: line %d is %d columns wide: %q",
					size[0], size[1], i, got, stripStyles(line))
			}
		}
	}
}

// btop writes the name of a box into its top edge with the digit that selects
// it, and that digit is the only thing on screen that says the panel can be
// selected at all. A panel the layout dropped names nothing, because it is not
// there to be named.
func TestEveryPlannedPanelIsNamedWithItsDigit(t *testing.T) {
	m := busyModel(time.Unix(1787433722, 0))

	for _, size := range [][2]int{{80, 24}, {120, 40}, {190, 60}} {
		m.width, m.height = size[0], size[1]
		got := stripStyles(m.renderFramed())
		plan := planLayout(size[0], size[1])

		// The label, not the word: "files" also appears in "1158/4667
		// files" and in the cache line, so a bare title would find a
		// panel that was never drawn.
		label := func(k panelKind) string {
			return fmt.Sprintf("%d %s", panels[k].hotkey, panels[k].title)
		}
		for _, p := range plan.panels {
			if want := label(p.kind); !strings.Contains(got, want) {
				t.Errorf("%dx%d: no panel labelled %q on screen", size[0], size[1], want)
			}
		}
		for _, k := range plan.dropped {
			if strings.Contains(got, label(k)) {
				t.Errorf("%dx%d: %v was dropped and drawn anyway", size[0], size[1], k)
			}
		}
	}
}

// The framed view is preset 1 and the dense one stays preset 0, which is what
// #7 settled. p is what gets between them -- and until it does, the whole view
// is code nothing reaches.
func TestPAlternatesBetweenTheTwoViews(t *testing.T) {
	m := busyModel(time.Unix(1787433722, 0))
	m.width, m.height = 120, 40

	framed := func(v string) bool { return strings.ContainsRune(stripStyles(v), '╭') }

	if framed(m.View()) {
		t.Error("the framed view came up by default; preset 0 is the dense one")
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = next.(Model)
	if !framed(m.View()) {
		t.Error("p did not reach the framed view")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = next.(Model)
	if framed(m.View()) {
		t.Error("p did not come back")
	}
}

// The graphs are budgeted against the terminal, and inside a frame the room is
// the panel's. Getting that wrong does not look like a bug: the line is cut to
// the frame, so the upload graph loses a few cells and the cumulative counters
// beside it disappear altogether -- a figure removed from the screen by the
// layout, which is the one thing CLAUDE.md's "every consumer must agree" rule
// about effectiveWidth exists to prevent.
func TestTheBandwidthPanelKeepsItsCountersWhenTheGraphsWillNotFit(t *testing.T) {
	m := busyModel(time.Unix(1787433722, 0))
	m.width, m.height = 120, 40
	m.graphs.resize(sparkCellsFor(effectiveWidth(m.width)), m.opts.GraphSymbol)
	for i := 0; i < 40; i++ {
		m.graphs.record(m.state.Processes)
	}

	got := stripStyles(m.renderFramed())
	for _, want := range []string{"rd 3.0 GiB", "wr 9.0 GiB"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q was pushed off the bandwidth panel:\n%s", want, got)
		}
	}
}

// The issue's opening complaint is "no meters", and a meter primitive with no
// call site does not answer it. A job whose completion is known gets a bar, in
// the panel that already prints the percentage beside it -- which is btop's
// arrangement, and the first thing on screen to use meter_bg.
func TestAJobWithKnownProgressGetsABar(t *testing.T) {
	m := busyModel(time.Unix(1787433722, 0))
	m.width, m.height = 120, 40

	// A run of cells, not a glyph: outside the tty palette a meter's two
	// halves are the same block in two colours, which is exactly why
	// --tty tells them apart by shape instead.
	lit, _ := meterGlyphs(m.opts.GraphSymbol)
	bar := strings.Repeat(string(lit), 8)

	got := stripStyles(m.renderFramed())
	if !strings.Contains(got, bar) {
		t.Errorf("no bar on screen -- expected a run of %q:\n%s", lit, got)
	}

	// And a run with no statistics behind it gets none: a bar at nought per
	// cent would be a claim about progress rather than a report of it, which
	// is the rule jobProgress already follows.
	m.state.Jobs = nil
	if bare := stripStyles(m.renderFramed()); strings.Contains(bare, bar) {
		t.Errorf("a job with no statistics was given a bar:\n%s", bare)
	}
}
