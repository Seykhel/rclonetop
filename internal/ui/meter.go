package ui

import (
	"math"
	"strings"

	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/box"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// meter draws btop's gradient bar: a fixed-width track with the leading part of
// it filled along a ramp.
//
// It goes through gradientStyle, which is the second and last thing entitled to,
// for the reason the colour rule gives: a meter is **area**. A dark cell against
// a dark background honestly reads as "not much", which is exactly what the cold
// end of a btop ramp is there to say -- the failure that rule exists to prevent
// is a dark *glyph*, which reads as nothing at all.
//
// The gradient runs across the bar rather than with the value, which is btop's
// own arrangement and the one that carries information: a cell's colour says
// where it sits on the scale, so the hot end always means "near the top" and
// never "this reading happens to be high". A bar that recoloured itself as it
// filled would say the same thing twice and mean neither.
//
// **A caller with no scale to divide by must not draw one.** frac is a fraction
// of something observed, and a meter drawn at zero because nothing has been
// measured yet is pixel-for-pixel a meter drawn at a measured zero -- the track
// is all there is to see either way. Unlike a graph, which simply stays blank,
// this one asserts an empty bar. Where the peak may not exist yet the caller
// says so in words, the way the throughput line already does.
func (m Model) meter(ramp string, frac float64, width int) string {
	if width < 1 {
		return ""
	}

	lit, track := meterGlyphs(m.opts.GraphSymbol)
	filled := meterFill(frac, width)

	var b strings.Builder
	b.Grow(width * 8)
	for i := 0; i < width; i++ {
		if i < filled {
			b.WriteString(m.gradientStyle(ramp, meterPoint(i, width)).Render(string(lit)))
			continue
		}
		// The unfilled part is a track, not a colder reading: meter_bg exactly,
		// which is what the key is in the theme for and what this is the first
		// use of.
		b.WriteString(m.style("meter_bg").Render(string(track)))
	}
	return b.String()
}

// meterColors is meter's colour arithmetic on its own, so what the bar means can
// be asserted on colours rather than inferred from escape sequences -- the same
// separation magnitudeColor keeps from magnitudeStyle. It must stay in step with
// the loop above, which is why both ask meterFill and meterPoint rather than
// each doing the sums.
func (m Model) meterColors(ramp string, frac float64, width int) []theme.Color {
	filled := meterFill(frac, width)

	colors := make([]theme.Color, max(width, 0))
	for i := range colors {
		if i >= filled {
			colors[i] = m.opts.Theme.Color("meter_bg")
			continue
		}
		colors[i] = m.gradientColor(ramp, meterPoint(i, width))
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

// meterPoint is where cell i sits along a bar of this width, as a fraction of
// the ramp. A one-cell meter has no span to grade and takes the hot end, which
// is where its only cell would sit if the bar were full.
func meterPoint(i, width int) float64 {
	if width <= 1 {
		return 1
	}
	return float64(i) / float64(width-1)
}

// meterGlyphs are the two cells a bar is built from.
//
// Away from a plain console both are the same solid block and only the colour
// differs, which is btop's look: one continuous bar with a dark track behind it.
// Under --tty they have to differ in shape as well, because a console font may
// not have U+2588 and, more to the point, the tty palette cannot be relied on to
// separate a filled cell from the track -- a bar that is always full says
// nothing.
func meterGlyphs(sym graph.Symbol) (lit, track rune) {
	if sym == graph.TTY {
		return '#', '-'
	}
	return '█', '█'
}

// boxRunes answers for a frame the question meterGlyphs answers for a bar, and
// it lives here beside it so that there is one place that reads GraphSymbol and
// not two.
//
// The box package cannot decide this for itself without importing graph, and a
// sub-package kept free of colour and of Bubble Tea should not start taking
// dependencies on its siblings to learn what the terminal can draw. So the
// decision is the renderer's, as it is for the meter.
func (m Model) boxRunes() box.Runes {
	if m.opts.GraphSymbol == graph.TTY {
		return box.ASCII
	}
	return box.Rounded
}
