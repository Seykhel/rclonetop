// Package graph turns a series of samples into rows of text.
//
// The glyphs are plotted directly rather than through a charting library. The
// requirement is narrow and specific -- btop's two-cell-tall braille graphs,
// degrading to blocks and then to plain ASCII -- and expressing it takes less
// code than adapting a general-purpose library would, with no dependency and
// exact control over the output. Colour is deliberately not applied here: Plot
// returns plain runes and the caller styles them, which keeps the glyph
// arithmetic testable as plain text and leaves room for a caller to grade a
// multi-row graph vertically the way btop's are.
package graph

import (
	"math"
	"strings"
)

// Symbol selects the glyph family, mirroring btop's graph_symbol option.
type Symbol string

const (
	// Braille packs two samples into every character cell, twice the
	// horizontal density of the others. Vertically a cell is four dots, so a
	// one-row graph has fewer steps than the eighth-blocks below, not more.
	// It is the default and needs a font with U+2800–U+28FF.
	Braille Symbol = "braille"
	// Block uses the eighth-block glyphs: one sample per cell, coarser, but
	// available in far more fonts.
	Block Symbol = "block"
	// TTY is plain ASCII, for a Linux console with no Unicode font at all.
	TTY Symbol = "tty"
)

// brailleDots maps a dot row within a cell, and a side, to its bit.
//
// Unicode numbers braille dots down the left column then down the right, with
// the fourth row added last for the eight-dot patterns:
//
//	(1) (4)     0x01 0x08
//	(2) (5)     0x02 0x10
//	(3) (6)     0x04 0x20
//	(7) (8)     0x40 0x80
var brailleDots = [4][2]byte{
	{0x01, 0x08},
	{0x02, 0x10},
	{0x04, 0x20},
	{0x40, 0x80},
}

// brailleRows is how many dot rows one character cell spans.
const brailleRows = 4

// blockRamp is the eighth-block ramp, from one eighth to full.
var blockRamp = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// ttyRamp is an ASCII density ramp for consoles without Unicode. Four filled
// levels is enough to tell a trickle from a saturated link, which is all TTY
// mode needs to do.
var ttyRamp = []rune{'.', '-', '=', '#'}

// SamplesPerCell reports how many samples one character cell represents.
// Callers size their history from this: a graph forty cells wide holds eighty
// braille samples but only forty block ones.
func SamplesPerCell(sym Symbol) int {
	if sym == Block || sym == TTY {
		return 1
	}
	return 2
}

// Plot renders samples as height rows of width cells, row 0 being the top.
//
// max is the value that fills the graph; samples above it clamp, which happens
// routinely because auto-scaling lags a sudden burst by a frame. When fewer
// samples are given than fit, they are right-aligned: the newest sample belongs
// on the right, so a graph fills from the right as history accumulates rather
// than appearing to run backwards.
func Plot(samples []float64, width, height int, max float64, sym Symbol) []string {
	if width < 1 || height < 1 {
		return nil
	}

	slots := width * SamplesPerCell(sym)
	// levels is the number of distinguishable steps up the whole graph.
	levels := height * brailleRows
	if sym == Block || sym == TTY {
		levels = height * len(blockRamp)
	}

	filled := fillLevels(samples, slots, levels, max)

	if sym == Block || sym == TTY {
		return plotRamp(filled, width, height, sym)
	}
	return plotBraille(filled, width, height)
}

// fillLevels converts samples into a per-slot count of filled steps, aligned to
// the right and padded with zeroes on the left.
func fillLevels(samples []float64, slots, levels int, max float64) []int {
	filled := make([]int, slots)
	if max <= 0 {
		// Nothing has been observed yet, so there is no scale to draw
		// against. Every slot stays empty rather than dividing by zero.
		return filled
	}

	// Keep only the samples that fit, taking them from the end.
	if len(samples) > slots {
		samples = samples[len(samples)-slots:]
	}
	offset := slots - len(samples)

	for i, v := range samples {
		switch {
		case math.IsNaN(v) || v <= 0:
			// NaN has to be caught explicitly: it compares false against
			// everything, so it would otherwise reach the arithmetic below,
			// where int(NaN) is the smallest int64 and underflows the glyph
			// index into a solid block.
			continue
		case v >= max:
			filled[offset+i] = levels
		default:
			n := int(v/max*float64(levels) + 0.5)
			// Any traffic at all must leave a mark. Rounding to nearest
			// erases everything below half a step, and one row of braille is
			// only four steps tall, so a transfer running at a tenth of the
			// window's peak would draw exactly as blank as a dead one -- while
			// the figure printed beside it says otherwise.
			if n < 1 {
				n = 1
			}
			if n > levels {
				n = levels
			}
			filled[offset+i] = n
		}
	}
	return filled
}

// plotBraille assembles the braille rows.
func plotBraille(filled []int, width, height int) []string {
	total := height * brailleRows

	rows := make([]string, height)
	for row := 0; row < height; row++ {
		var b strings.Builder
		b.Grow(width * 3) // braille runes are three bytes

		for cell := 0; cell < width; cell++ {
			var mask byte
			for side := 0; side < 2; side++ {
				n := filled[cell*2+side]
				for dot := 0; dot < brailleRows; dot++ {
					// Global dot row, counted from the top of the graph.
					g := row*brailleRows + dot
					// Filling grows upwards from the bottom.
					if total-g <= n {
						mask |= brailleDots[dot][side]
					}
				}
			}
			b.WriteRune(rune(0x2800 + int(mask)))
		}
		rows[row] = b.String()
	}
	return rows
}

// plotRamp assembles the block and ASCII rows, one sample per cell.
func plotRamp(filled []int, width, height int, sym Symbol) []string {
	steps := len(blockRamp)

	rows := make([]string, height)
	for row := 0; row < height; row++ {
		// Rows are numbered from the top, but a bar grows from the bottom, so
		// the lowest row is the first to fill.
		band := height - 1 - row

		var b strings.Builder
		b.Grow(width * 3)

		for cell := 0; cell < width; cell++ {
			within := filled[cell] - band*steps
			switch {
			case within <= 0:
				b.WriteRune(' ')
			case sym == TTY:
				// Compress the eight block steps onto four ASCII ones.
				i := (min(within, steps) - 1) * len(ttyRamp) / steps
				b.WriteRune(ttyRamp[i])
			default:
				b.WriteRune(blockRamp[min(within, steps)-1])
			}
		}
		rows[row] = b.String()
	}
	return rows
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
