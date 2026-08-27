package ui

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/box"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

func TestMeterFillIsProportional(t *testing.T) {
	cases := []struct {
		frac  float64
		width int
		want  int
	}{
		{0, 20, 0},
		{0.5, 10, 5},
		{1, 10, 10},
		{0.25, 20, 5},
		// Rounding to nearest, like the graph's own steps.
		{0.34, 10, 3},
		{0.36, 10, 4},
	}
	for _, c := range cases {
		if got := meterFill(c.frac, c.width); got != c.want {
			t.Errorf("meterFill(%v, %d) = %d, want %d", c.frac, c.width, got, c.want)
		}
	}
}

// The rule graph.fillLevels already follows, for the same reason: a mount
// trickling along at a hundredth of the scale draws exactly as blank as a dead
// one, while the figure printed beside it says otherwise.
func TestAnyNonZeroValueLeavesAMark(t *testing.T) {
	for _, frac := range []float64{0.001, 0.01, 0.04} {
		if got := meterFill(frac, 20); got != 1 {
			t.Errorf("meterFill(%v, 20) = %d, want 1", frac, got)
		}
	}
	// And nothing is still nothing: zero is a measurement, not a rounding.
	if got := meterFill(0, 20); got != 0 {
		t.Errorf("meterFill(0, 20) = %d, want 0", got)
	}
}

// The scale a meter is drawn against is an observed peak, and a fresh sample
// can exceed it before the peak catches up. It clamps rather than overflowing
// the bar into the line beside it.
func TestMeterFillClampsAndRejectsNonsense(t *testing.T) {
	if got := meterFill(1.4, 10); got != 10 {
		t.Errorf("above full = %d, want 10", got)
	}
	for _, frac := range []float64{-0.5, math.NaN()} {
		if got := meterFill(frac, 10); got != 0 {
			t.Errorf("meterFill(%v, 10) = %d, want 0", frac, got)
		}
	}
	if got := meterFill(0.5, 0); got != 0 {
		t.Errorf("a meter of no width filled %d cells", got)
	}
}

func meterModel(sym graph.Symbol) Model {
	lipgloss.SetColorProfile(0) // Ascii: no escape sequences to strip
	return New(nil, Options{Theme: theme.Default(), GraphSymbol: sym}, nil)
}

// A meter is exactly as wide as it was asked for, filled or not: the empty part
// is a track, not absent. That track is what meter_bg is for, and it is the
// first thing in this program to use it.
func TestAMeterIsAllTrackAndAlwaysItsFullWidth(t *testing.T) {
	m := meterModel(graph.Braille)

	for _, frac := range []float64{0, 0.3, 1} {
		got := stripStyles(m.meter("cpu", frac, 12))
		if lipgloss.Width(got) != 12 {
			t.Errorf("frac %v gave %d cells: %q", frac, lipgloss.Width(got), got)
		}
		if strings.Contains(got, " ") {
			t.Errorf("frac %v left a hole in the track: %q", frac, got)
		}
	}
}

// The two halves have to be tellable apart without colour, or the tty theme
// shows a bar that is always full.
func TestTheTTYMeterSaysHowFullItIsInASCII(t *testing.T) {
	m := meterModel(graph.TTY)

	got := stripStyles(m.meter("cpu", 0.5, 10))
	if want := "#####-----"; got != want {
		t.Errorf("meter = %q, want %q", got, want)
	}
	for _, r := range got {
		if r > 127 {
			t.Errorf("a non-ASCII rune %q survived the fallback", r)
		}
	}
}

// The gradient runs across the bar, not with the value: a cell's colour says
// where it sits, so the hot end means "near the top of the scale" and not "this
// reading happens to be high". That is btop's own arrangement, and it is why
// the fill can be indexed raw -- a meter is area, and a dark cell against a
// dark background honestly reads as "not much".
func TestTheGradientRunsAcrossTheBarNotWithTheValue(t *testing.T) {
	m := meterModel(graph.Braille)

	half := m.meterColors("cpu", 0.5, 10)
	full := m.meterColors("cpu", 1, 10)

	if half[0] != full[0] || half[4] != full[4] {
		t.Error("a cell changed colour because the value changed, not because it moved")
	}
	if half[0] == half[9] {
		t.Error("the two ends of the bar came out the same colour")
	}
	// The far end is the ramp's own hot end, whatever the reading.
	if want := m.opts.Theme.Gradient("cpu", 1); full[9] != want {
		t.Errorf("the last cell is %v, want the ramp's end %v", full[9], want)
	}
}

// The unfilled cells are meter_bg exactly, not a faded ramp: it is a track
// behind the bar rather than a colder reading.
func TestTheEmptyTrackIsMeterBg(t *testing.T) {
	m := meterModel(graph.Braille)

	colors := m.meterColors("cpu", 0.5, 10)
	track := m.opts.Theme.Color("meter_bg")
	for i := 5; i < 10; i++ {
		if colors[i] != track {
			t.Errorf("cell %d is %v, want meter_bg %v", i, colors[i], track)
		}
	}
	if colors[4] == track {
		t.Error("the last filled cell was drawn as track")
	}
}

func TestAMeterWithNoRoomDrawsNothing(t *testing.T) {
	m := meterModel(graph.Braille)
	for _, width := range []int{0, -3} {
		if got := m.meter("cpu", 0.5, width); got != "" {
			t.Errorf("width %d gave %q", width, got)
		}
	}
}

// The frame and the bar have to answer the same question the same way, and the
// answer is the renderer's: box cannot read GraphSymbol without importing a
// sibling package, and the whole point of it is not to. One place reads the
// option for both, so a layout author cannot get one right and the other wrong.
func TestTheFrameDegradesWithTheBar(t *testing.T) {
	if got := meterModel(graph.TTY).boxRunes(); got != box.ASCII {
		t.Errorf("tty frame = %+v, want the ASCII runes", got)
	}
	for _, sym := range []graph.Symbol{graph.Braille, graph.Block, ""} {
		if got := meterModel(sym).boxRunes(); got != box.Rounded {
			t.Errorf("%q frame = %+v, want the rounded runes", sym, got)
		}
		if lit, _ := meterGlyphs(sym); lit == '#' {
			t.Errorf("%q got the ASCII bar", sym)
		}
	}
}
