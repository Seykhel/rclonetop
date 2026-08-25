package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// meter draws btop's gradient bar: a fixed-width track with the leading part of
// it filled along a ramp.
//
// This is the second thing in the program entitled to index a ramp raw, and for
// the reason the colour rule gives: a meter is **area**. A dark cell against a
// dark background honestly reads as "not much", which is exactly what the cold
// end of a btop ramp is there to say -- the failure that rule exists to prevent
// is a dark *glyph*, which reads as nothing at all.
//
// The gradient runs across the bar rather than with the value, which is btop's
// own arrangement and the one that carries information: a cell's colour says
// where it sits on the scale, so the hot end always means "near the top" and
// never "this reading happens to be high". A bar that recoloured itself as it
// filled would say the same thing twice and mean neither.
func (m Model) meter(ramp string, frac float64, width int) string {
	if width < 1 {
		return ""
	}

	filled, empty := meterGlyphs(m.opts.GraphSymbol)
	colors := m.meterColors(ramp, frac, width)
	n := meterFill(frac, width)

	var b strings.Builder
	b.Grow(width * 8)
	for i := 0; i < width; i++ {
		cell := empty
		if i < n {
			cell = filled
		}
		b.WriteString(lipgloss.NewStyle().Foreground(colors[i].Lipgloss()).Render(string(cell)))
	}
	return b.String()
}

// meterColors is meter's colour arithmetic on its own, so what the bar means can
// be asserted on colours rather than inferred from escape sequences -- the same
// separation magnitudeColor keeps from magnitudeStyle.
func (m Model) meterColors(ramp string, frac float64, width int) []theme.Color {
	n := meterFill(frac, width)

	colors := make([]theme.Color, max(width, 0))
	for i := range colors {
		if i >= n {
			// The unfilled part is a track, not a colder reading: meter_bg
			// exactly, which is what the key is in the theme for and what this
			// is the first use of.
			colors[i] = m.opts.Theme.Color("meter_bg")
			continue
		}
		// Position along the whole bar, not along the filled part, so the
		// colours do not slide about as the value moves. A one-cell meter has
		// no span to grade and takes the hot end, which is where its only cell
		// would sit if the bar were full.
		at := 1.0
		if width > 1 {
			at = float64(i) / float64(width-1)
		}
		colors[i] = m.opts.Theme.Gradient(ramp, at)
	}
	return colors
}

// meterFill is how many cells of width are lit at frac.
//
// Rounded to nearest, then floored at one for any positive fraction at all --
// the rule graph.fillLevels already follows, and for the same reason: a mount
// trickling along at a hundredth of the scale would otherwise draw exactly as
// blank as a dead one, while the figure printed beside it says otherwise. Zero
// itself stays zero, because that is a measurement rather than a rounding.
func meterFill(frac float64, width int) int {
	if width < 1 || math.IsNaN(frac) || frac <= 0 {
		// NaN has to be caught by name: it compares false against everything,
		// so it would reach the arithmetic below and index the bar with the
		// smallest int64.
		return 0
	}
	if frac >= 1 {
		return width
	}
	n := int(frac*float64(width) + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

// meterGlyphs are the two cells a bar is built from.
//
// Away from a plain console both are the same solid block and only the colour
// differs, which is btop's look: one continuous bar with a dark track behind it.
// Under --tty they have to differ in shape as well, because a console font may
// not have U+2588 and, more to the point, the tty palette cannot be relied on to
// separate a filled cell from the track -- a bar that is always full says
// nothing.
func meterGlyphs(sym graph.Symbol) (filled, empty rune) {
	if sym == graph.TTY {
		return '#', '-'
	}
	return '█', '█'
}
