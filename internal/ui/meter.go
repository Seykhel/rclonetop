package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui/box"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

// meter draws btop's gradient bar: a fixed-width track with the leading part of
// it filled along a ramp.
//
// A meter is **area**, so its fill indexes a ramp rather than blending one, which
// is what the colour rule allows area to do. It goes one step further than the
// sparkline does, through meterStyle, because area against a *track* is not the
// same problem as area against the background -- see there.
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
			b.WriteString(m.meterStyle(ramp, meterPoint(i, width)).Render(string(lit)))
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
		colors[i] = m.meterColor(ramp, meterPoint(i, width))
	}
	return colors
}

// meterFloor is how far above the track a filled cell has to sit, in Rec. 709
// luminance. Roughly a tenth of the full range: the smallest step that reads as
// a different colour rather than as a rendering artefact.
const meterFloor = 24

// meterStyle paints one filled cell. It is the meter's gradientStyle, and it
// exists because gradientStyle alone is not enough here.
//
// The colour rule says a ramp may be indexed raw to fill area, on the grounds
// that a dark cell against a dark background honestly reads as "not much". True,
// and load-bearing for the sparkline -- but a meter's cell is not against the
// background. It is against meter_bg, a *lighter* dark, and against that the
// cold end of five of the seven ramps is darker than the track it sits in. At
// four per cent the bar did not show a mark, it showed a hole: exactly the
// failure the text rule exists to prevent, arrived from the other side.
//
// So: indexed raw, then lifted if it does not clear the track. Only the cells
// that fail are touched, and each by the least that works, so the ramp keeps its
// own colours everywhere it was already legible -- the hot end included.
func (m Model) meterStyle(ramp string, at float64) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.meterColor(ramp, at).Lipgloss())
}

// meterColor is meterStyle's arithmetic on its own, so the property that matters
// can be asserted on a colour rather than on an escape sequence.
//
// The lift is towards the ramp's own hot end rather than towards main_fg,
// because a download cell has to stay recognisably a download cell; blending to
// grey would buy visibility by throwing away what the colour was saying.
// Luminance is linear under Blend, so the smallest sufficient blend is solved
// rather than searched for.
func (m Model) meterColor(ramp string, at float64) theme.Color {
	c := m.gradientColor(ramp, at)
	want := luminance(m.opts.Theme.Color("meter_bg")) + meterFloor
	have := luminance(c)
	if have >= want {
		return c
	}

	hot := m.gradientColor(ramp, 1)
	if luminance(hot) <= have {
		// A ramp whose hot end is no brighter than this cell -- cpu runs green
		// to red and loses luminance doing it. There is nothing to lift towards,
		// and such a ramp is bright at the cold end anyway, so it never gets
		// here in the built-in themes.
		return c
	}

	// Aimed one unit above the floor, because Blend rounds each channel to a
	// byte and the exact solution can land a fraction short of what it solved
	// for. One unit of luminance is the most a single channel's rounding can
	// cost, so this cannot undershoot.
	t := (want + 1 - have) / (luminance(hot) - have)
	if t > 1 {
		// The whole ramp is darker than the track wants. Best effort, and the
		// tty palette is where this happens: eight saturated colours whose
		// luminance lies about them, which is why that mode tells the two halves
		// apart by shape as well.
		t = 1
	}
	return theme.Blend(c, hot, t)
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
