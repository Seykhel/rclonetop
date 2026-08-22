package graph

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// repeat builds a flat series of n samples all at v.
func repeat(v float64, n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func TestBrailleBlankAndFull(t *testing.T) {
	// A silent link must draw as blank braille rather than as a baseline of
	// dots, or an idle transfer looks like a busy one.
	rows := Plot(repeat(0, 8), 4, 2, 100, Braille)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for i, row := range rows {
		if row != strings.Repeat("⠀", 4) {
			t.Errorf("row %d = %q, want four blank braille cells", i, row)
		}
	}

	// At full scale every dot is set: U+28FF.
	rows = Plot(repeat(100, 8), 4, 2, 100, Braille)
	for i, row := range rows {
		if row != strings.Repeat("⣿", 4) {
			t.Errorf("row %d = %q, want four full braille cells", i, row)
		}
	}
}

func TestBrailleHalfHeightFillsFromTheBottom(t *testing.T) {
	// One cell is four dot rows tall. Half scale fills the lower two, which in
	// the Unicode dot numbering is dots 3 and 7 on the left column and 6 and 8
	// on the right: 0x04|0x40|0x20|0x80 = 0xE4.
	rows := Plot(repeat(50, 2), 1, 1, 100, Braille)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0] != "⣤" {
		t.Errorf("got %q (U+%04X), want U+28E4", rows[0], []rune(rows[0])[0])
	}
}

func TestBrailleTwoSamplesPerCell(t *testing.T) {
	// Two cells wide holds four samples. Only the last is at full height, so
	// only the right column of the last cell is fully set.
	rows := Plot([]float64{0, 0, 0, 100}, 2, 1, 100, Braille)
	got := []rune(rows[0])
	if len(got) != 2 {
		t.Fatalf("got %d cells, want 2", len(got))
	}
	if got[0] != '⠀' {
		t.Errorf("first cell = U+%04X, want U+2800", got[0])
	}
	// Right column, all four dot rows: 0x08|0x10|0x20|0x80 = 0xB8.
	if got[1] != '⢸' {
		t.Errorf("second cell = U+%04X, want U+28B8", got[1])
	}
	if SamplesPerCell(Braille) != 2 {
		t.Errorf("SamplesPerCell(Braille) = %d, want 2", SamplesPerCell(Braille))
	}
}

func TestNewestSamplesSitOnTheRight(t *testing.T) {
	// A partly filled history must be right-aligned. Padding on the right
	// instead would make a graph appear to run backwards as it fills.
	rows := Plot([]float64{100, 100}, 4, 1, 100, Braille)
	got := []rune(rows[0])
	for i := 0; i < 3; i++ {
		if got[i] != '⠀' {
			t.Errorf("cell %d = U+%04X, want blank padding", i, got[i])
		}
	}
	if got[3] != '⣿' {
		t.Errorf("last cell = U+%04X, want U+28FF", got[3])
	}
}

func TestBlockMode(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		want  rune
	}{
		{"silent", 0, ' '},
		{"half", 50, '▄'},  // 4 eighths
		{"full", 100, '█'}, // 8 eighths
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := Plot(repeat(tt.value, 1), 1, 1, 100, Block)
			if got := []rune(rows[0])[0]; got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	if SamplesPerCell(Block) != 1 {
		t.Errorf("SamplesPerCell(Block) = %d, want 1", SamplesPerCell(Block))
	}
}

func TestBlockStacksAcrossRows(t *testing.T) {
	// Two cells tall at three quarters: the top row is half full and the
	// bottom row is solid.
	rows := Plot(repeat(75, 1), 1, 2, 100, Block)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	if got := []rune(rows[0])[0]; got != '▄' {
		t.Errorf("top row = %q, want ▄", got)
	}
	if got := []rune(rows[1])[0]; got != '█' {
		t.Errorf("bottom row = %q, want █", got)
	}
}

func TestTTYModeIsASCIIOnly(t *testing.T) {
	// The point of TTY mode is a Linux console with no Unicode font. A single
	// multi-byte rune defeats it.
	for _, v := range []float64{0, 10, 25, 50, 75, 100} {
		rows := Plot(repeat(v, 6), 6, 2, 100, TTY)
		for _, row := range rows {
			if utf8.RuneCountInString(row) != len(row) {
				t.Errorf("value %v produced non-ASCII output: %q", v, row)
			}
			for _, r := range row {
				if r > 127 {
					t.Errorf("value %v produced rune U+%04X", v, r)
				}
			}
		}
	}
	// It still has to distinguish loud from quiet.
	quiet := Plot(repeat(5, 2), 2, 1, 100, TTY)[0]
	loud := Plot(repeat(100, 2), 2, 1, 100, TTY)[0]
	if quiet == loud {
		t.Errorf("TTY mode renders 5%% and 100%% identically as %q", quiet)
	}
}

// TestAnyTrafficLeavesAMark is a regression test.
//
// One row of braille is four steps tall, so rounding to nearest erased every
// sample below an eighth of the scale. A mount downloading a mebibyte per
// second while uploading ten kibibytes drew the upload exactly as blank as an
// idle process, contradicting the figure printed next to it.
func TestAnyTrafficLeavesAMark(t *testing.T) {
	scale := 1024.0 * 1024
	for _, sym := range []Symbol{Braille, Block, TTY} {
		for _, rate := range []float64{1, 1024, 10 * 1024, 100 * 1024} {
			got := Plot(repeat(rate, 32), 16, 1, scale, sym)[0]
			if strings.TrimSpace(strings.Trim(got, "⠀")) == "" {
				t.Errorf("%s at %.0f B/s on a %.0f B/s scale drew blank: %q",
					sym, rate, scale, got)
			}
		}
	}

	// A burst followed by steady traffic: the steady part must stay visible
	// even though the burst sets the scale ten times higher.
	samples := append([]float64{10 << 20}, repeat(1<<20, 31)...)
	got := Plot(samples, 16, 1, 10<<20, Braille)[0]
	if strings.Count(got, "⠀") > 2 {
		t.Errorf("steady traffic after a burst drew mostly blank: %q", got)
	}
}

func TestNaNSamplesAreIgnored(t *testing.T) {
	// int(NaN) is the smallest int64, which underflowed the glyph index and
	// painted a solid column above an empty cell.
	rows := Plot([]float64{math.NaN()}, 1, 2, 100, Block)
	for i, row := range rows {
		if row != " " {
			t.Errorf("row %d = %q, want a blank cell", i, row)
		}
	}
	if got := Plot([]float64{math.NaN()}, 1, 1, 100, Braille)[0]; got != "⠀" {
		t.Errorf("braille got %q, want blank", got)
	}
	// Infinities are already handled by the comparisons, but pin them down.
	// Two samples, because one braille cell holds two and a single sample
	// would fill only the right-hand column.
	if got := Plot(repeat(math.Inf(1), 2), 1, 1, 100, Braille)[0]; got != "⣿" {
		t.Errorf("+Inf got %q, want a full cell", got)
	}
	if got := Plot(repeat(math.Inf(-1), 2), 1, 1, 100, Braille)[0]; got != "⠀" {
		t.Errorf("-Inf got %q, want blank", got)
	}
}

func TestValuesAboveMaxClamp(t *testing.T) {
	// Auto-scaling lags a sudden burst by one frame, so samples above the
	// current maximum are normal and must not overflow the glyph table.
	rows := Plot(repeat(500, 2), 1, 1, 100, Braille)
	if rows[0] != "⣿" {
		t.Errorf("got %q, want a fully set cell", rows[0])
	}
	if got := []rune(Plot(repeat(500, 1), 1, 1, 100, Block)[0])[0]; got != '█' {
		t.Errorf("block mode got %q, want █", got)
	}
}

func TestNegativeAndZeroScale(t *testing.T) {
	// A max of zero means nothing has been observed yet. Dividing by it would
	// produce NaN and, once cast, a nonsense glyph index.
	for _, max := range []float64{0, -1} {
		rows := Plot(repeat(10, 2), 2, 1, max, Braille)
		if rows[0] != strings.Repeat("⠀", 2) {
			t.Errorf("max %v: got %q, want blanks", max, rows[0])
		}
	}
	// Negative samples cannot happen from a byte counter, but clamping them is
	// cheaper than trusting that.
	if rows := Plot(repeat(-5, 2), 2, 1, 100, Braille); rows[0] != strings.Repeat("⠀", 2) {
		t.Errorf("negative samples: got %q, want blanks", rows[0])
	}
}

func TestDegenerateDimensions(t *testing.T) {
	// The renderer is called with whatever the terminal reports, including
	// nothing at all during a resize.
	for _, d := range []struct{ w, h int }{{0, 1}, {1, 0}, {0, 0}, {-3, -3}} {
		rows := Plot(repeat(50, 4), d.w, d.h, 100, Braille)
		for _, row := range rows {
			if row == "" {
				continue
			}
			t.Errorf("width %d height %d produced %q, want no output", d.w, d.h, row)
		}
	}
}

func TestEmptySeries(t *testing.T) {
	rows := Plot(nil, 3, 2, 100, Braille)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row != strings.Repeat("⠀", 3) {
			t.Errorf("got %q, want blanks", row)
		}
	}
}

func TestUnknownSymbolFallsBackToBraille(t *testing.T) {
	// The symbol comes from a configuration file, so it can be anything.
	if got, want := Plot(repeat(100, 2), 1, 1, 100, Symbol("nonsense")), "⣿"; got[0] != want {
		t.Errorf("got %q, want %q", got[0], want)
	}
	if SamplesPerCell(Symbol("nonsense")) != 2 {
		t.Error("an unknown symbol should behave like braille")
	}
}
