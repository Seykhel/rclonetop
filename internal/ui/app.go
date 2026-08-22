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
	return Model{
		opts:    opts,
		state:   model.NewState(),
		results: results,
		now:     time.Now(),
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
func (m Model) gradientStyle(ramp string, frac float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.opts.Theme.Gradient(ramp, frac).Lipgloss())
}
