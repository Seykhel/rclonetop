package ui

import (
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/series"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

const (
	// rateFieldWidth is the column the figure is padded to. It keeps the two
	// directions aligned so the line does not jitter as the numbers change
	// width, and ten is enough for the widest rate worth printing ("999 MiB/s").
	rateFieldWidth = 10

	// throughputTextWidth is how many columns the throughput line needs for
	// its text alone: the two arrows with their padded figures, the
	// separators, and the cumulative totals. Measured from the rendered line,
	// with headroom for totals that reach four digits.
	throughputTextWidth = 58

	// minSparkCells is the narrowest graph worth drawing. Below this the
	// graphs are dropped entirely rather than squeezed, because two or three
	// cells convey nothing and the line would still overflow.
	minSparkCells = 6

	// maxSparkCells caps the history on a very wide terminal.
	//
	// Sixteen rather than something larger because a graph is mostly blank
	// whenever the link is mostly idle -- which is the normal state of a mount
	// -- and a wide one reads as a gap between the figure and its own trace
	// rather than as a trace at all.
	maxSparkCells = 16
)

// sparkCellsFor divides the space left over on the throughput line between the
// two graphs, returning zero when there is not enough for either.
//
// Every other renderer in the dense view budgets against the terminal width;
// the graphs used to be a fixed sixteen cells, which pushed the line to
// ninety-two columns and wrapped it on the eighty-column terminal that is both
// the commonest and this view's own fallback.
func sparkCellsFor(width int) int {
	cells := (width - throughputTextWidth) / 2
	switch {
	case cells < minSparkCells:
		return 0
	case cells > maxSparkCells:
		return maxSparkCells
	default:
		return cells
	}
}

// graphStore keeps the recent throughput of each process.
//
// The time axis follows the process collector's own cadence of one second, not
// --update: the +/- keys change how often the screen is redrawn, but not how
// fast history accumulates. A graph therefore spans roughly its width in
// seconds, doubled in braille.
//
// It lives outside model.State because it is a property of the display, not an
// observation: the collectors report an instantaneous rate and know nothing
// about how much history the terminal happens to be able to show.
type graphStore struct {
	read  map[int]*series.Ring
	write map[int]*series.Ring

	// startedAt is the identity of the process each ring belongs to. A PID is
	// not unique over time: the kernel recycles them, and a one-shot rclone
	// launched by a timer can inherit the number of one that has just exited.
	// Without this the new process would adopt the old one's history and, worse,
	// its scale -- another process's throughput attributed to it in silence.
	startedAt map[int]time.Time

	// cells is the graph width in character cells, and capacity how many
	// samples that comes to for the symbol in use.
	cells    int
	capacity int
}

func newGraphStore(symbol graph.Symbol, cells int) *graphStore {
	return &graphStore{
		read:      make(map[int]*series.Ring),
		write:     make(map[int]*series.Ring),
		startedAt: make(map[int]time.Time),
		cells:     cells,
		capacity:  cells * graph.SamplesPerCell(symbol),
	}
}

// record appends the current rates and forgets processes that have exited.
//
// Only processes whose counters could actually be read contribute: pushing a
// zero for a process whose /proc/<pid>/io is unreadable would draw a flat line
// that looks like a measurement of silence.
func (g *graphStore) record(procs []model.Process) {
	live := make(map[int]bool, len(procs))

	for _, p := range procs {
		live[p.PID] = true
		if !p.IOAvailable {
			continue
		}

		prev, known := g.startedAt[p.PID]
		if known && !prev.Equal(p.StartedAt) {
			// A different process wearing the same number. Its predecessor's
			// history says nothing about it.
			delete(g.read, p.PID)
			delete(g.write, p.PID)
			known = false
		}
		if !known {
			// The first rate reported for any process is a placeholder, not a
			// measurement: the collector had no earlier sample to subtract
			// from and returned zero. Recording it would put a notch of false
			// silence at the left edge of every new graph.
			g.startedAt[p.PID] = p.StartedAt
			g.ringFor(g.read, p.PID)
			g.ringFor(g.write, p.PID)
			continue
		}

		g.ringFor(g.read, p.PID).Push(p.ReadRate)
		g.ringFor(g.write, p.PID).Push(p.WriteRate)
	}

	for pid := range g.read {
		if !live[pid] {
			delete(g.read, pid)
			delete(g.write, pid)
			delete(g.startedAt, pid)
		}
	}
}

func (g *graphStore) ringFor(m map[int]*series.Ring, pid int) *series.Ring {
	r, ok := m[pid]
	if !ok {
		r = series.New(g.capacity)
		m[pid] = r
	}
	return r
}

// resize changes how much history is kept when the terminal is resized or the
// graph symbol changes, keeping the newest samples that still fit.
func (g *graphStore) resize(cells int, symbol graph.Symbol) {
	capacity := cells * graph.SamplesPerCell(symbol)
	g.cells = cells
	if capacity == g.capacity {
		return
	}
	g.capacity = capacity
	if capacity < 1 {
		// The terminal is too narrow for a graph. The rings are kept so the
		// history survives a temporary squeeze.
		return
	}
	for _, r := range g.read {
		r.Resize(capacity)
	}
	for _, r := range g.write {
		r.Resize(capacity)
	}
}

// scaleFor returns the value that fills a process's graphs.
//
// The scale is the largest rate in the window and nothing else, which is what
// btop's net_auto does. An absolute floor was tried and was wrong: with a floor
// of one mebibyte per second, a mount genuinely moving ten kibibytes per second
// rounds to zero dots and draws as flat as an idle one. The sparkline's job is
// to show the shape of the activity, and the magnitude is already spelled out
// in the figure printed beside it.
//
// Both directions share the scale, which is btop's net_sync: with separate
// scales a trickle of uploads would be drawn as tall as a saturated download
// and the two could no longer be compared at a glance.
//
// Zero means nothing moved, and Plot renders that as blank.
func (g *graphStore) scaleFor(pid int) float64 {
	scale := 0.0
	if r, ok := g.read[pid]; ok {
		if m := r.Max(g.capacity); m > scale {
			scale = m
		}
	}
	if w, ok := g.write[pid]; ok {
		if m := w.Max(g.capacity); m > scale {
			scale = m
		}
	}
	return scale
}

// spark renders one direction's history as a single row of glyphs. It returns
// nothing when the terminal is too narrow to give the graph any room.
func (g *graphStore) spark(m map[int]*series.Ring, pid int, symbol graph.Symbol) string {
	r, ok := m[pid]
	if !ok || g.cells < 1 {
		return ""
	}
	rows := graph.Plot(r.Window(g.capacity), g.cells, 1, g.scaleFor(pid), symbol)
	if len(rows) == 0 {
		return ""
	}
	return rows[0]
}
