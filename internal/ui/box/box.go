// Package box draws the frame around a btop-style panel.
//
// It exists because lipgloss stops one step short. `RoundedBorder()` supplies
// the six runes and will wrap a block of text in them, but it has nowhere to put
// a title: btop writes the name of every box *into* its top edge, and that edge
// then has to come out to exactly the width it was given whatever the name is.
// Composing it by hand is a dozen lines; bending a border style into doing it is
// not.
//
// Colour is deliberately absent, on the same terms as internal/ui/graph. The top
// edge comes back as segments rather than as a finished string so the caller can
// paint the border, the title and the hotkey in three different colours without
// parsing them back out of one -- and so that all of the arithmetic here stays
// testable as plain text.
package box

import "strconv"

// Runes are the six pieces a frame is drawn from.
type Runes struct {
	TopLeft, TopRight       rune
	BottomLeft, BottomRight rune
	Horizontal, Vertical    rune
}

var (
	// Rounded is btop's own frame.
	Rounded = Runes{'╭', '╮', '╰', '╯', '─', '│'}

	// ASCII is the fallback for a console with no Unicode font, on the same
	// principle as graph.ttyRamp: the shape survives, the glyphs degrade. A
	// caller picks it for the same reason it picks graph.TTY, and from the same
	// answer.
	ASCII = Runes{'+', '+', '+', '+', '-', '|'}
)

// Kind says what a segment of the top edge is, so a renderer can colour it
// without knowing where the geometry put it.
type Kind int

const (
	KindBorder Kind = iota
	KindTitle
	KindHotkey
)

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
	return max0(b.Width - 2), max0(b.Height - 2)
}

// Top returns the top edge in pieces, left to right. A hotkey of zero means the
// box has none.
//
// The title costs more than its own length: a leading rune so it does not start
// on the corner, a space either side, and at least one trailing rune so it does
// not end on the other corner. When all of that does not fit, the title is
// dropped rather than cut -- "ba…" names no box, the frame's own colour already
// says which one this is, and a truncated word reads as a rendering fault rather
// than as a narrow terminal.
func (b Box) Top(hotkey int, title string) []Segment {
	if !b.drawable() {
		return nil
	}

	key := ""
	if hotkey != 0 {
		key = strconv.Itoa(hotkey)
	}

	// corners + leading rune + the two spaces + one trailing rune, and the key
	// brings its own separating space.
	cost := 2 + 1 + 2 + 1 + len([]rune(title)) + len(key)
	if key != "" {
		cost++
	}

	if title == "" || cost > b.Width {
		// The hotkey goes with the title it names: a bare digit labels nothing,
		// which is worse than an unlabelled box.
		return []Segment{{Kind: KindBorder, Text: string(b.Runes.TopLeft) +
			repeat(b.Runes.Horizontal, b.Width-2) + string(b.Runes.TopRight)}}
	}

	segs := []Segment{{Kind: KindBorder, Text: string(b.Runes.TopLeft) + string(b.Runes.Horizontal) + " "}}
	if key != "" {
		segs = append(segs,
			Segment{Kind: KindHotkey, Text: key},
			Segment{Kind: KindBorder, Text: " "})
	}
	segs = append(segs,
		Segment{Kind: KindTitle, Text: title},
		Segment{Kind: KindBorder, Text: " " +
			repeat(b.Runes.Horizontal, b.Width-cost+1) + string(b.Runes.TopRight)})
	return segs
}

// Bottom is the closing edge, which carries nothing and is therefore one string.
func (b Box) Bottom() string {
	if !b.drawable() {
		return ""
	}
	return string(b.Runes.BottomLeft) +
		repeat(b.Runes.Horizontal, b.Width-2) + string(b.Runes.BottomRight)
}

func repeat(r rune, n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
