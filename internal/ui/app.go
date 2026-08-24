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
}

// Model is the root Bubble Tea model.
type Model struct {
	opts    Options
	state   *model.State
	results <-chan collect.Result

	width  int
	height int
	now    time.Time

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
		// The graphs share the throughput line with fixed text, so their
		// width -- and therefore how much history is worth keeping -- follows
		// the terminal.
		m.graphs.resize(sparkCellsFor(effectiveWidth(m.width)), m.opts.GraphSymbol)
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
// This is the *area* colour, and only area may use it: a filled graph cell or a
// meter segment. btop's ramps begin dark on purpose -- download_start is
// #291f75 and used_start is #592b26 -- because a dark filled cell reads as "not
// much" against the background. A dark *glyph* reads as nothing at all.
// Anything that draws letters wants magnitudeStyle instead.
func (m Model) gradientStyle(ramp string, frac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.opts.Theme.Gradient(ramp, frac).Lipgloss())
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
func (m Model) magnitudeStyle(ramp string, frac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.magnitudeColor(ramp, frac).Lipgloss())
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

// label is the styling for the words that name a value -- "pid", "up", "rss".
//
// They were inactive_fg, which is #40 in the built-in theme: dark grey chosen to
// mean "this is switched off". Using it for ordinary labels made two thirds of
// the screen nearly invisible and left nothing to say "switched off" with.
// inactive_fg is now reserved for what is genuinely inert or stale, and the
// hierarchy between a label and its value is carried by weight instead --
// labels plain, values bold. That distinction survives on a terminal with eight
// colours, which a distinction made of colour would not.
func (m Model) label() lipgloss.Style {
	return m.style("main_fg")
}

// value is the styling for a figure that carries no magnitude of its own -- a
// PID, a thread count, a duration.
func (m Model) value() lipgloss.Style {
	return m.style("main_fg").Bold(true)
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
