// Package box draws the frame around a btop-style panel.
//
// It exists because lipgloss stops one step short. Its RoundedBorder() will wrap
// a block of text in these same six runes, but it has nowhere to put a title:
// btop writes the name of every box *into* its top edge, and that edge then has
// to come out to exactly the width it was given whatever the name is. Composing
// it by hand is a dozen lines; bending a border style into doing it is not.
//
// The runes below are copied rather than imported for that reason. Depending on
// lipgloss for six characters would put this package back inside the thing it is
// kept outside of: colour is deliberately absent here, on the same terms as
// internal/ui/graph, which is what keeps all of the arithmetic testable as plain
// text.
//
// The top edge comes back as segments rather than as a finished string so the
// caller can paint the border, the title and the hotkey in three different
// colours without parsing them back out of one.
//
// What this does not do is compose a row of content. Padding one to Inner()
// needs the display width of a string that already carries colour, and knowing
// that is exactly the dependency this package does without.
package box

import (
	"strconv"
	"strings"
)

// Runes are the pieces a frame is drawn from: six for the frame itself, two
// that bracket the title where it is cut into the top edge, and whether the
// hotkey is written in superscript.
type Runes struct {
	TopLeft, TopRight       rune
	BottomLeft, BottomRight rune
	Horizontal, Vertical    rune
	// TitleLeft and TitleRight are where the edge turns aside to let the name
	// through. btop draws them as the two corners that face each other, ┐ and
	// ┌, so the title reads as set into the frame rather than written over a
	// gap in it -- which is what a pair of spaces looked like.
	TitleLeft, TitleRight rune
	// Superscript writes the hotkey as ¹ rather than 1, flush against the
	// name, which is btop's own spelling. It is a property of the rune set
	// because a console font is exactly the one that may not have the glyphs:
	// there the digit goes back to the baseline with a space after it.
	Superscript bool
}

var (
	// Rounded is btop's own frame.
	Rounded = Runes{'╭', '╮', '╰', '╯', '─', '│', '┐', '┌', true}

	// ASCII is the fallback for a console with no Unicode font, on the same
	// principle as graph.ttyRamp: the shape survives, the glyphs degrade. A
	// caller picks it for the same reason it picks graph.TTY, and from the same
	// answer.
	ASCII = Runes{'+', '+', '+', '+', '-', '|', '[', ']', false}
)

// superscripts spells the hotkey digits the way btop does. Indexed by digit, so
// a multi-digit key is written one rune at a time and every one of them is a
// single cell.
var superscripts = [10]rune{'⁰', '¹', '²', '³', '⁴', '⁵', '⁶', '⁷', '⁸', '⁹'}

// Kind says what a segment of the top edge is, so a renderer can colour it
// without knowing where the geometry put it.
type Kind int

const (
	KindBorder Kind = iota
	KindTitle
	KindHotkey
)

// NoHotkey is the absence of one, named so that a caller does not have to write
// a bare zero and a reader does not have to guess what it meant.
const NoHotkey = 0

// Segment is one run of the top edge with one meaning.
type Segment struct {
	Text string
	Kind Kind
}

// Box is the geometry of one framed panel.
type Box struct {
	Width, Height int
	Runes         Runes
}

// drawable reports whether there is room for a frame at all. Below two columns
// the corners would overlap each other, and below two rows the top edge and the
// bottom edge would be the same row.
func (b Box) drawable() bool { return b.Width >= 2 && b.Height >= 2 }

// Inner is the area left for content once the frame has taken its two columns
// and its two rows. It never goes negative, so a caller sizing a slice from it
// does not have to check.
func (b Box) Inner() (width, height int) {
	return max(b.Width-2, 0), max(b.Height-2, 0)
}

// Top returns the top edge in pieces, left to right. NoHotkey means the box has
// none.
//
// The title costs more than its own length: a leading rune so it does not start
// on the corner, a bracket either side, and at least one trailing rune so
// it does not end on the other corner. When all of that does not fit, the title is
// dropped rather than cut -- "ba…" names no box, the frame's own colour already
// says which one this is, and a truncated word reads as a rendering fault rather
// than as a narrow terminal.
func (b Box) Top(title string, hotkey int) []Segment {
	if !b.drawable() {
		return nil
	}

	key := ""
	if hotkey != NoHotkey {
		key = b.hotkey(hotkey)
	}

	// corners + leading rune + the two brackets + one trailing rune. A key on
	// the baseline brings its own separating space; a superscript one sits
	// flush against the name, which is what lets it go without one.
	cost := 2 + 1 + 2 + 1 + len([]rune(title)) + len([]rune(key))
	if key != "" && !b.Runes.Superscript {
		cost++
	}

	if title == "" || cost > b.Width {
		// The hotkey goes with the title it names: a bare digit labels nothing,
		// which is worse than an unlabelled box.
		return []Segment{{Kind: KindBorder, Text: string(b.Runes.TopLeft) +
			b.rule(b.Width-2) + string(b.Runes.TopRight)}}
	}

	segs := []Segment{{Kind: KindBorder, Text: string(b.Runes.TopLeft) +
		string(b.Runes.Horizontal) + string(b.Runes.TitleLeft)}}
	if key != "" {
		segs = append(segs, Segment{Kind: KindHotkey, Text: key})
		if !b.Runes.Superscript {
			segs = append(segs, Segment{Kind: KindBorder, Text: " "})
		}
	}
	segs = append(segs,
		Segment{Kind: KindTitle, Text: title},
		Segment{Kind: KindBorder, Text: string(b.Runes.TitleRight) +
			b.rule(b.Width-cost+1) + string(b.Runes.TopRight)})
	return segs
}

// hotkey spells a key in this rune set's digits. A negative key has no
// superscript minus worth reaching for and no caller that would pass one, so it
// keeps the baseline spelling rather than inventing a glyph.
func (b Box) hotkey(key int) string {
	s := strconv.Itoa(key)
	if !b.Runes.Superscript || key < 0 {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, d := range s {
		out = append(out, superscripts[d-'0'])
	}
	return string(out)
}

// Bottom is the closing edge, which carries nothing and is therefore one string.
func (b Box) Bottom() string {
	if !b.drawable() {
		return ""
	}
	return string(b.Runes.BottomLeft) +
		b.rule(b.Width-2) + string(b.Runes.BottomRight)
}

// rule is a run of the horizontal rune. Every caller is inside drawable(), which
// is what makes n safe: the corners account for the only two columns that can
// take the count below zero.
func (b Box) rule(n int) string {
	return strings.Repeat(string(b.Runes.Horizontal), n)
}
