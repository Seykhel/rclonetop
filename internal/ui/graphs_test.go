package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// testCells is the graph width the tests work at, wide enough to be
// representative without depending on the terminal-width arithmetic.
const testCells = 16

var testStart = time.Unix(1787000000, 0)

// feed pushes n recorded samples of one process at the given rates.
//
// It records n+1 times because the first observation of any process carries a
// placeholder rate rather than a measurement, and record deliberately drops it.
func feed(g *graphStore, pid int, read, write float64, n int) {
	for i := 0; i <= n; i++ {
		g.record([]model.Process{{
			PID: pid, StartedAt: testStart,
			IOAvailable: true, ReadRate: read, WriteRate: write,
		}})
	}
}

func newTestStore() *graphStore { return newGraphStore(graph.Braille, testCells) }

// brailleWeight counts set dots, as a proxy for how tall a graph is drawn.
func brailleWeight(s string) int {
	n := 0
	for _, r := range s {
		for mask := r - 0x2800; mask > 0; mask >>= 1 {
			n += int(mask & 1)
		}
	}
	return n
}

// TestSmallButRealRatesAreVisible is a regression test.
//
// The scale used to be floored at one mebibyte per second, so a mount moving a
// few kibibytes drew as blank — indistinguishable from idle, while the figure
// printed next to it said otherwise.
func TestSmallButRealRatesAreVisible(t *testing.T) {
	g := newTestStore()
	feed(g, 1, 11*1024, 0, g.capacity)

	got := g.spark(g.read, 1, graph.Braille, g.cells)
	if got == "" {
		t.Fatal("no sparkline was produced")
	}
	if strings.Trim(got, "⠀") == "" {
		t.Errorf("a steady 11 KiB/s drew as blank: %q", got)
	}
}

func TestIdleProcessDrawsBlank(t *testing.T) {
	// The converse: nothing moving must not be amplified into a full graph.
	g := newTestStore()
	feed(g, 1, 0, 0, g.capacity)

	if got := g.spark(g.read, 1, graph.Braille, g.cells); strings.Trim(got, "⠀") != "" {
		t.Errorf("an idle process drew %q, want blanks", got)
	}
}

func TestDirectionsShareOneScale(t *testing.T) {
	// btop's net_sync: a trickle of uploads alongside a flood of downloads must
	// stay visibly smaller, not be rescaled up to match.
	g := newTestStore()
	feed(g, 1, 1_000_000, 10_000, g.capacity)

	read := g.spark(g.read, 1, graph.Braille, g.cells)
	write := g.spark(g.write, 1, graph.Braille, g.cells)
	if read == write {
		t.Errorf("a 100:1 difference rendered identically: %q", read)
	}

	// Smaller, but not erased. An earlier version of this test asserted the
	// quiet direction drew blank, which enshrined the very bug it was meant to
	// guard against: at a hundredth of the shared scale the upload graph became
	// byte-for-byte identical to an idle one, while the figure beside it read
	// 10 KiB/s.
	if strings.Trim(write, "⠀") == "" {
		t.Error("the smaller direction was erased entirely")
	}
	if brailleWeight(write) >= brailleWeight(read) {
		t.Errorf("the smaller direction is not drawn lower: %q vs %q", write, read)
	}
}

func TestUnreadableCountersProduceNoSamples(t *testing.T) {
	// Pushing zeroes for a process whose counters cannot be read would draw a
	// flat line that looks like a measurement of silence.
	g := newTestStore()
	for i := 0; i < 4; i++ {
		g.record([]model.Process{{PID: 7, StartedAt: testStart, IOAvailable: false}})
	}
	if got := g.spark(g.read, 7, graph.Braille, g.cells); got != "" {
		t.Errorf("got %q, want no sparkline at all", got)
	}
}

// TestFirstSampleIsDropped covers the placeholder rate every process reports
// once. The collector has nothing to subtract from on its first observation and
// returns zero, which is not a measurement of silence.
func TestFirstSampleIsDropped(t *testing.T) {
	g := newTestStore()
	g.record([]model.Process{{PID: 1, StartedAt: testStart, IOAvailable: true}})

	if r, ok := g.read[1]; !ok {
		t.Fatal("the ring should exist after the first observation")
	} else if r.Len() != 0 {
		t.Errorf("the placeholder rate was recorded: Len = %d, want 0", r.Len())
	}

	g.record([]model.Process{{PID: 1, StartedAt: testStart, IOAvailable: true, ReadRate: 100}})
	if got := g.read[1].Len(); got != 1 {
		t.Errorf("the second observation was not recorded: Len = %d, want 1", got)
	}
}

// TestRecycledPIDStartsFresh guards the same hazard the process collector
// already guards: the kernel reuses PIDs, so a one-shot rclone can inherit the
// number of one that just exited. Adopting its history would attribute another
// process's throughput to it, and its scale along with it.
func TestRecycledPIDStartsFresh(t *testing.T) {
	g := newTestStore()
	feed(g, 1, 5_000_000, 0, g.capacity)
	if g.read[1].Len() == 0 {
		t.Fatal("expected history for the first process")
	}

	later := testStart.Add(time.Hour)
	g.record([]model.Process{{PID: 1, StartedAt: later, IOAvailable: true, ReadRate: 1}})

	if got := g.read[1].Len(); got != 0 {
		t.Errorf("the new process inherited %d samples of history", got)
	}
	if got := g.scaleFor(1); got != 0 {
		t.Errorf("the new process inherited a scale of %v", got)
	}
}

func TestExitedProcessesAreForgotten(t *testing.T) {
	g := newTestStore()
	feed(g, 1, 100, 100, 3)
	feed(g, 2, 100, 100, 3)

	// Only process 2 is still running.
	g.record([]model.Process{{PID: 2, StartedAt: testStart, IOAvailable: true, ReadRate: 100}})

	if _, ok := g.read[1]; ok {
		t.Error("the history of an exited process was kept")
	}
	if _, ok := g.write[1]; ok {
		t.Error("the write history of an exited process was kept")
	}
	if _, ok := g.startedAt[1]; ok {
		t.Error("the identity of an exited process was kept")
	}
	if _, ok := g.read[2]; !ok {
		t.Error("the history of a live process was dropped")
	}
}

func TestResizeFollowsTheSymbol(t *testing.T) {
	// Braille carries two samples per cell, the other modes one, so switching
	// symbol changes how much history is worth keeping.
	g := newTestStore()
	if g.capacity != testCells*2 {
		t.Fatalf("capacity = %d, want %d", g.capacity, testCells*2)
	}
	feed(g, 1, 100, 100, g.capacity)

	g.resize(testCells, graph.Block)
	if g.capacity != testCells {
		t.Errorf("capacity = %d, want %d", g.capacity, testCells)
	}
	if got := g.read[1].Cap(); got != testCells {
		t.Errorf("the existing ring was not resized: cap = %d", got)
	}
}

// TestSparkCellsFollowTheTerminal covers the sizing that keeps the throughput
// line inside the terminal. A fixed sixteen cells pushed it to ninety-two
// columns, which wraps on the eighty-column terminal the layout is tuned for.
func TestSparkCellsFollowTheTerminal(t *testing.T) {
	// Derived from the constants rather than written out, so tightening the
	// layout does not turn this into a table of stale magic numbers.
	narrowest := throughputTextWidth + 2*minSparkCells

	tests := []struct {
		width int
		want  int
	}{
		{1000, maxSparkCells}, // capped
		{throughputTextWidth + 2*maxSparkCells, maxSparkCells},
		{narrowest, minSparkCells}, // exactly the minimum
		{narrowest - 1, 0},         // below it, dropped not squeezed
		{40, 0},
		{0, 0},
		{-1, 0},
	}
	for _, tt := range tests {
		if got := sparkCellsFor(tt.width); got != tt.want {
			t.Errorf("sparkCellsFor(%d) = %d, want %d", tt.width, got, tt.want)
		}
	}

	// A graph should not sprawl on a wide terminal: mostly-blank cells read as
	// a gap between the figure and its own trace.
	if got := sparkCellsFor(300); got > maxSparkCells {
		t.Errorf("sparkCellsFor(300) = %d, want at most %d", got, maxSparkCells)
	}

	// The whole point: text plus both graphs must fit.
	for _, width := range []int{narrowest, 80, 100, 120, 200, 400} {
		total := throughputTextWidth + 2*sparkCellsFor(width)
		if total > width {
			t.Errorf("at width %d the line needs %d columns", width, total)
		}
	}
}

func TestNarrowTerminalDropsTheGraph(t *testing.T) {
	g := newGraphStore(graph.Braille, sparkCellsFor(throughputTextWidth))
	feed(g, 1, 1000, 1000, 4)
	if got := g.spark(g.read, 1, graph.Braille, g.cells); got != "" {
		t.Errorf("got %q, want no graph on a narrow terminal", got)
	}

	// Widening brings it back, with the history that survived the squeeze.
	g.resize(sparkCellsFor(120), graph.Braille)
	if got := g.spark(g.read, 1, graph.Braille, g.cells); got == "" {
		t.Error("the graph did not come back after widening")
	}
}

// TestZeroWidthFallsBackNotOff checks that the renderer and the graph sizing
// agree on what a terminal reporting no width means. They disagreed once: the
// renderer assumed eighty columns while the sizing read it as too narrow to
// draw, so the graphs vanished with no explanation.
func TestZeroWidthFallsBackNotOff(t *testing.T) {
	if effectiveWidth(0) != defaultWidth || effectiveWidth(-5) != defaultWidth {
		t.Fatalf("effectiveWidth does not fall back to %d", defaultWidth)
	}
	if got := sparkCellsFor(effectiveWidth(0)); got < minSparkCells {
		t.Errorf("an unreported width yields %d cells, so the graphs disappear", got)
	}
}
