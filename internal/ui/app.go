// Package ui renders the collected state as a terminal interface.
//
// The default view is deliberately dense: no frames, a handful of tightly
// packed lines that fit a narrow tmux pane. What it borrows from btop is not
// the box drawing but the colour language -- values are graded along the
// theme's gradients so magnitude is legible before the number is read.
package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// Version is stamped at build time by the release tooling.
var Version = "0.1.0-dev"

// Options configures a Model. The names mirror btop's configuration keys where
// the meaning is the same, so anyone who has tuned btop already knows them.
type Options struct {
	Theme       *theme.Theme
	UpdateMS    int
	Base10      bool
	Host        string
	ClockLayout string
	GraphSymbol graph.Symbol
	// Preset is the view to start in: 0 for the dense one, 1 for the framed
	// one -- mirrored by Model.preset, which p then alternates at runtime.
	Preset int
	// ShownBoxes is internal/config's raw shown_boxes value, carried through
	// unresolved the same way GraphSymbol is: interpreting it (empty means
	// every panel, an unrecognised name is dropped) is parseShownBoxes's job,
	// not this package's caller's.
	ShownBoxes string
}

// Model is the root Bubble Tea model.
type Model struct {
	opts    Options
	state   *model.State
	results <-chan collect.Result

	width  int
	height int
	now    time.Time

	// preset is which view is on screen: 0 is the dense one, 1 the framed
	// one. btop's own numbering, and the two values are the two that exist --
	// which is the condition #7 set for the flag that names them.
	preset int

	// shown is which framed-view panels are candidates for the screen right
	// now. Seeded once from Options.ShownBoxes at construction; a future
	// session-only digit toggle mutates it from here, never from the
	// configuration it was seeded from.
	shown shown

	// peakRate is the largest throughput seen so far, used as the upper
	// bound when grading a rate along the gradient. It auto-scales like
	// btop's net_auto rather than assuming a link speed rclonetop cannot
	// know.
	peakRate float64

	// graphs holds the per-process history the sparklines are drawn from. It
	// is a pointer because Update takes the model by value.
	graphs *graphStore

	quitting bool
	cancel   context.CancelFunc
}

// resultMsg carries one collector result into the update loop.
type resultMsg collect.Result

// tickMsg drives the clock and the uptime counters, which must advance even
// when no collector has anything new to say.
type tickMsg time.Time

// New builds the root model around a stream of collector results.
func New(results <-chan collect.Result, opts Options, cancel context.CancelFunc) Model {
	if opts.Theme == nil {
		opts.Theme = theme.Default()
	}
	if opts.UpdateMS <= 0 {
		opts.UpdateMS = 2000
	}
	if opts.ClockLayout == "" {
		opts.ClockLayout = "15:04:05"
	}
	if opts.GraphSymbol == "" {
		opts.GraphSymbol = graph.Braille
	}
	return Model{
		opts:    opts,
		state:   model.NewState(),
		results: results,
		now:     time.Now(),
		preset:  opts.Preset,
		shown:   parseShownBoxes(opts.ShownBoxes),
		graphs:  newGraphStore(opts.GraphSymbol, sparkCellsFor(effectiveWidth(0))),
		cancel:  cancel,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(waitFor(m.results), tick(m.opts.UpdateMS))
}

// waitFor blocks on the collector channel in a command, which is how a Bubble
// Tea program consumes an external stream without polling.
func waitFor(ch <-chan collect.Result) tea.Cmd {
	return func() tea.Msg {
		r, ok := <-ch
		if !ok {
			return nil
		}
		return resultMsg(r)
	}
}

func tick(ms int) tea.Cmd {
	return tea.Tick(time.Duration(ms)*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.graphs.resize(m.graphCells(), m.opts.GraphSymbol)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tickMsg:
		m.now = time.Time(msg)
		return m, tick(m.opts.UpdateMS)

	case resultMsg:
		res := collect.Result(msg)
		if res.Err != nil {
			m.state.Fail(res.Source, res.Err)
		} else {
			m.state.Apply(res.Snapshot)
			m.trackPeak()
			// Only the collector that reports processes advances the
			// graphs. Sampling on every collector's tick would stretch the
			// time axis by however many sources happen to be enabled.
			if res.Snapshot.Processes != nil {
				m.graphs.record(res.Snapshot.Processes)
			}
		}
		return m, waitFor(m.results)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc":
		m.quitting = true
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	case "p":
		// Alternates rather than counts up: with two presets those are
		// the same thing, and a counter would need a modulus that means
		// nothing until there is a third.
		m.preset = 1 - m.preset
		// And the history is resized with it. The two views can show
		// wildly different amounts of it -- sixteen cells on a dense
		// line against a panel-wide graph -- so a store sized for the
		// one the user just left draws the other blank down its right
		// half, or throws away history it was about to need.
		m.graphs.resize(m.graphCells(), m.opts.GraphSymbol)
	case "+", "=":
		m.opts.UpdateMS = clampInterval(m.opts.UpdateMS / 2)
	case "-", "_":
		m.opts.UpdateMS = clampInterval(m.opts.UpdateMS * 2)
	}
	return m, nil
}

func clampInterval(ms int) int {
	switch {
	case ms < 100:
		return 100
	case ms > 30000:
		return 30000
	default:
		return ms
	}
}

// graphCells is how many cells of history the view on screen can show, and
// therefore how much of it is worth keeping.
//
// It asks the layout rather than assuming, because the answer is the bandwidth
// panel's width in the framed view and the leftover on the throughput line in
// the dense one, and because a framed view on a terminal too small for frames is
// the dense view. Every caller of resize goes through here: the two that matter
// are a window resize and the p key, and they have to agree.
func (m Model) graphCells() int {
	if m.preset == 1 {
		// The demand does not change which panel is where, only how tall
		// each one is, and this only wants the bandwidth panel's width.
		if plan := planLayout(m.width, m.height, panelRows{}, m.shown); !plan.dense {
			for _, p := range plan.panels {
				if p.kind == panelBandwidth {
					// What the frame leaves, less the arrow's
					// indent, is the trace.
					w, _ := p.inner()
					return w - graphIndent
				}
			}
		}
	}
	return sparkCellsFor(effectiveWidth(m.width))
}

// trackPeak keeps the gradient's upper bound in step with what this host
// actually achieves. Without it every rate on a slow link would sit at the cold
// end of the ramp and carry no information.
func (m *Model) trackPeak() {
	for _, p := range m.state.Processes {
		if r := p.ReadRate + p.WriteRate; r > m.peakRate {
			m.peakRate = r
		}
	}
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	if m.preset == 1 {
		// Which may still hand back the dense view: a terminal with no
		// room for frames gets the one that fits, and the preset is left
		// alone so that widening the window restores what was asked for.
		return m.renderFramed()
	}
	return m.renderDense()
}

// style is the shortcut used throughout the renderers: a foreground colour
// taken from the active theme.
func (m Model) style(key string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.opts.Theme.Color(key).Lipgloss())
}

// gradientStyle grades a value along a named ramp. frac is clamped by the
// theme, so callers can pass a raw ratio.
//
// Callers do not reach for this directly except to fill area -- a graph cell or
// a meter segment. Text goes through magnitudeStyle when the fraction is
// measured and accentStyle when it was chosen, and the difference between those
// two is where the bug lived: btop's ramps begin dark on purpose
// (download_start is #291f75) because a dark filled cell reads as "not much"
// against the background, which is true, while a dark glyph reads as nothing at
// all.
func (m Model) gradientStyle(ramp string, frac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.gradientColor(ramp, frac).Lipgloss())
}

// gradientColor is gradientStyle's arithmetic on its own, so an area fill can be
// asserted on a colour rather than on an escape sequence -- the same split
// magnitudeColor and accentColor already keep from the styles above them.
func (m Model) gradientColor(ramp string, frac float64) theme.Color {
	return m.opts.Theme.Gradient(ramp, frac)
}

// luminance is Rec. 709 relative brightness, which is what decides whether one
// colour can be read against another. Plain channel averages would call #4f43a3
// and #43a34f equally legible, and the eye does not.
//
// It is a poor proxy for a saturated primary -- it scores #ff0000 at 54 -- which
// is why every rule built on it exempts the tty palette, eight saturated colours
// where the number lies.
func luminance(c theme.Color) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

// magnitudeStyle grades text by magnitude without ever painting it in the dark
// end of a ramp.
//
// Indexing a ramp directly is right for area and wrong for text, and the
// difference is the whole reason this exists. An idle mount has frac 0, so
// rateCell used to write "↓ 0 B/s" in download_start -- near-black violet on a
// dark background, a measurement the user cannot read. Every label on screen
// suffered a milder version of it: 36 MiB of resident memory is 0.035 of the
// 1 GiB saturation point, which came out a dull maroon.
//
// So the ramp is not indexed for the colour, it is blended *towards*. At frac 0
// the text is plain main_fg and perfectly legible; as the magnitude climbs it
// takes on more of the ramp until, at 1, it is the ramp's own hot end. The
// value reads at a glance exactly as btop's does, and it reads at all when
// there is nothing to report -- which for a backup monitor is most of the time.
//
// theme.Blend does the mixing and already existed for fadedAlarm, which cools an
// error towards inactive_fg as it ages. Same idea, opposite direction.
// It comes back bold, and callers do not get a say. "Bold rides with colour and
// only with colour" was a sentence in CLAUDE.md and a .Bold(true) repeated at
// fourteen call sites, with not one of them wanting it otherwise. A rule with no
// exceptions belongs in the constructor, where it cannot be forgotten by the
// fifteenth.
func (m Model) magnitudeStyle(ramp string, frac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.magnitudeColor(ramp, frac).Lipgloss()).Bold(true)
}

// magnitudeColor is magnitudeStyle's arithmetic on its own, so the property
// that matters can be asserted on a colour rather than inferred from an escape
// sequence.
func (m Model) magnitudeColor(ramp string, frac float64) theme.Color {
	// Blend clamps neither argument, and a rate divided by an observed peak can
	// exceed one on the sample that sets a new peak. Gradient clamps its own
	// index; this has to clamp the mixing fraction, or the arithmetic runs past
	// the ramp and comes back out the other side.
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	return theme.Blend(
		m.opts.Theme.Color("main_fg"),
		m.opts.Theme.Gradient(ramp, frac),
		frac)
}

// labelDim is how far a label is faded from the body text towards inactive_fg.
//
// Halfway, and both ends of that are deliberate. inactive_fg on its own is #40
// in the built-in theme -- dark grey, chosen to mean "switched off" -- and using
// it for the words that name a value made two thirds of the screen nearly
// invisible while leaving nothing to say "switched off" with. main_fg on its own
// is the other failure: a label indistinguishable from the figure beside it.
// Halfway is legible and plainly secondary, which is all a label has to be.
const labelDim = 0.5

// label is the styling for the words that name a value -- "pid", "up", "rss".
func (m Model) label() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.labelColor().Lipgloss())
}

// labelColor is label's arithmetic on its own, so the three-way ordering it has
// to sit in the middle of can be asserted on colours.
func (m Model) labelColor() theme.Color {
	return theme.Blend(
		m.opts.Theme.Color("main_fg"),
		m.opts.Theme.Color("inactive_fg"),
		labelDim)
}

// value is the styling for a figure that carries no magnitude of its own -- a
// PID, a thread count, a cumulative byte counter.
//
// Not bold. Bold rides with colour in this view: it belongs to magnitudeStyle
// and accentStyle, the two that mean "this number has a size worth noticing".
// Emphasising every figure emphasises none of them, and the first attempt at
// this made the whole screen bold and flat.
func (m Model) value() lipgloss.Style {
	return m.style("main_fg")
}

// accent is a fixed point on a ramp, chosen to colour text.
//
// The distinction from magnitudeStyle is the one that matters, and it is not
// "text versus area" as first written. It is **measured versus chosen**. A
// measurement reaches zero -- an idle mount reports exactly that for hours --
// and the zero end of a btop ramp is unreadable, so a measurement has to be
// blended. A fixed point is a colour decision somebody made and looked at
// ("cache figures are cyan"), and blending it only dilutes it: doing so turned
// the cache sizes from saturated cyan into a pale wash for no gain.
type accent struct {
	ramp string
	at   float64
}

var (
	accentCacheSize = accent{"cached", 0.6}
	// Well past the middle of a ramp that is dark for most of its length: the
	// same figure at 0.35 came out a muddy maroon, which was the one fixed point
	// that genuinely needed moving rather than blending.
	accentSyncSize = accent{"used", 0.8}
	accentActive   = accent{"free", 0.4}
	accentRunning  = accent{"free", 1}
	accentBusy     = accent{"cpu", 0.5}
	accentFailed   = accent{"temp", 1}

	// The arrows that say which of two stacked graphs is which. The hot end
	// of the graph's own ramp, so the label and the trace under it are the
	// same colour -- and a chosen point, which is why they are declared here
	// and walked by the legibility test rather than reached for inline.
	// Colour alone cannot do this job: --tty has eight of them.
	accentDownload = accent{"download", 1}
	accentUpload   = accent{"upload", 1}
)

// textAccents is what the legibility test walks. An accent added without being
// listed here is the one way an unreadable one gets past the suite.
var textAccents = []accent{
	accentCacheSize, accentSyncSize, accentActive,
	accentRunning, accentBusy, accentFailed,
	accentDownload, accentUpload,
}

// accentStyle colours text at a fixed, chosen point on a ramp. Bold for the same
// reason magnitudeStyle is, and built in for the same reason.
func (m Model) accentStyle(a accent) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.accentColor(a).Lipgloss()).Bold(true)
}

// alarm is the styling for something wrong that is wrong right now -- a stopped
// timer, a non-zero exit, a count of errors. hi_fg and bold, which is the same
// bargain the two above strike: the colour says what kind of thing it is, the
// weight says it is worth reading before the line beside it.
//
// fadedAlarm is the other half of this and the distinction matters: it cools the
// same colour towards inactive_fg as an error ages, because a failure from
// yesterday and a failure from a minute ago are not the same news.
func (m Model) alarm() lipgloss.Style {
	return m.style("hi_fg").Bold(true)
}

// accentColor is accentStyle's arithmetic on its own, so every chosen point can
// be held to the same legibility floor the blend guarantees for a measurement.
func (m Model) accentColor(a accent) theme.Color {
	return m.opts.Theme.Gradient(a.ramp, a.at)
}

// defaultWidth is what the dense view assumes before the terminal has reported
// its size. Eighty columns is both the conventional default and the width the
// layout is tuned against.
const defaultWidth = 80

// effectiveWidth resolves the width every renderer should budget against.
//
// A terminal can report zero, both before the first size message and from
// harnesses that do not allocate a real one. Every consumer has to agree on
// what that means: the renderer treating it as eighty while the graph sizing
// treated it as "too narrow to draw" silently dropped the graphs.
func effectiveWidth(w int) int {
	if w <= 0 {
		return defaultWidth
	}
	return w
}
