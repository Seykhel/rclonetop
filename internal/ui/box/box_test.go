package box

import (
	"strings"
	"testing"
)

// join is what a renderer would produce if it coloured every segment the same,
// which is exactly what the geometry has to be right about.
func join(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

func cells(s string) int { return len([]rune(s)) }

func TestAFrameWithNoTitle(t *testing.T) {
	b := Box{Width: 10, Height: 3, Runes: Rounded}

	if got, want := join(b.Top(0, "")), "╭────────╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
	if got, want := b.Bottom(), "╰────────╯"; got != want {
		t.Errorf("bottom = %q, want %q", got, want)
	}
	if got, want := string(b.Runes.Vertical), "│"; got != want {
		t.Errorf("side = %q, want %q", got, want)
	}
}

// The title sits inside the border, which is the whole reason this package
// exists: lipgloss draws a rounded frame but has nowhere to put a name in it.
func TestTheTitleSitsInsideTheTopEdge(t *testing.T) {
	b := Box{Width: 20, Height: 3, Runes: Rounded}

	if got, want := join(b.Top(0, "files")), "╭─ files ──────────╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
	if cells(join(b.Top(0, "files"))) != b.Width {
		t.Errorf("the edge is not %d cells wide: %q", b.Width, join(b.Top(0, "files")))
	}
}

// btop numbers its boxes, and the digit is coloured differently from the name.
func TestTheHotkeyPrecedesTheTitle(t *testing.T) {
	b := Box{Width: 20, Height: 3, Runes: Rounded}

	if got, want := join(b.Top(2, "files")), "╭─ 2 files ────────╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
}

// The pieces come back separately because the renderer paints them in three
// different colours -- border, title, hotkey -- and asking it to find them
// again inside a finished string is how a geometry bug becomes a colour bug.
func TestTheSegmentsAreHandedOverSeparately(t *testing.T) {
	b := Box{Width: 24, Height: 3, Runes: Rounded}

	var got []string
	for _, s := range b.Top(3, "bandwidth") {
		got = append(got, kindName(s.Kind)+":"+s.Text)
	}
	want := []string{"border:╭─ ", "hotkey:3", "border: ", "title:bandwidth", "border: ────────╮"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("segments =\n  %v\nwant\n  %v", got, want)
	}
}

// A console with no Unicode font gets the same geometry in ASCII, on the same
// principle as graph.ttyRamp: the shape survives, the glyphs degrade.
func TestTheASCIIFallbackKeepsTheShape(t *testing.T) {
	b := Box{Width: 20, Height: 3, Runes: ASCII}

	if got, want := join(b.Top(1, "files")), "+- 1 files --------+"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
	if got, want := b.Bottom(), "+------------------+"; got != want {
		t.Errorf("bottom = %q, want %q", got, want)
	}
	for _, r := range join(b.Top(1, "files")) + b.Bottom() {
		if r > 127 {
			t.Errorf("a non-ASCII rune %q survived the fallback", r)
		}
	}
}

// A title that cannot be shown whole is dropped rather than cut. "ba…" names no
// box, and the frame's own colour already says which one this is -- whereas a
// truncated word reads as a rendering fault.
func TestATitleThatDoesNotFitIsDroppedNotCut(t *testing.T) {
	b := Box{Width: 12, Height: 3, Runes: Rounded}

	if got, want := join(b.Top(0, "bandwidth")), "╭──────────╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
	// Three columns wider and it fits, with the single trailing rune that keeps
	// the name off the corner. "bandwidth" is nine, and the frame charges six
	// more for the corners, the leading rune, the two spaces and that trailing
	// one.
	wider := Box{Width: 15, Height: 3, Runes: Rounded}
	if got, want := join(wider.Top(0, "bandwidth")), "╭─ bandwidth ─╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
}

// The hotkey goes with the title. Half a label -- a bare digit naming nothing --
// is worse than no label at all.
func TestTheHotkeyGoesWithTheTitleItNames(t *testing.T) {
	b := Box{Width: 14, Height: 3, Runes: Rounded}

	if got, want := join(b.Top(4, "bandwidth")), "╭────────────╮"; got != want {
		t.Errorf("top = %q, want %q", got, want)
	}
}

func TestInnerIsWhatIsLeftAfterTheFrame(t *testing.T) {
	b := Box{Width: 20, Height: 6, Runes: Rounded}
	if w, h := b.Inner(); w != 18 || h != 4 {
		t.Errorf("inner = %dx%d, want 18x4", w, h)
	}

	// Never negative: a caller sizing a slice from this must not have to check.
	for _, small := range []Box{{Width: 0, Height: 0}, {Width: 1, Height: 1}, {Width: 2, Height: 2}} {
		if w, h := small.Inner(); w < 0 || h < 0 {
			t.Errorf("%dx%d gave inner %dx%d", small.Width, small.Height, w, h)
		}
	}
}

// Below two columns or two rows there is no frame to draw -- the corners alone
// would overlap -- and saying so is the layout's cue to drop the box rather
// than to draw a broken one.
func TestABoxTooSmallToFrameDrawsNothing(t *testing.T) {
	for _, b := range []Box{
		{Width: 1, Height: 3, Runes: Rounded},
		{Width: 10, Height: 1, Runes: Rounded},
		{Width: 0, Height: 0, Runes: Rounded},
		{Width: -4, Height: 3, Runes: Rounded},
	} {
		if got := b.Top(0, "files"); got != nil {
			t.Errorf("%dx%d gave a top edge: %q", b.Width, b.Height, join(got))
		}
		if got := b.Bottom(); got != "" {
			t.Errorf("%dx%d gave a bottom edge: %q", b.Width, b.Height, got)
		}
	}
}

// The one property that has to hold everywhere: an edge is exactly as wide as
// the box, whatever is written into it. A single cell over and every line below
// wraps; a single cell under and the right-hand border walks left as the title
// changes.
func TestEveryEdgeIsExactlyTheBoxWidth(t *testing.T) {
	titles := []string{"", "a", "files", "bandwidth", "a title far longer than any box"}
	for width := 2; width <= 120; width++ {
		for _, title := range titles {
			for _, key := range []int{0, 1, 9} {
				for _, runes := range []Runes{Rounded, ASCII} {
					b := Box{Width: width, Height: 3, Runes: runes}
					if got := cells(join(b.Top(key, title))); got != width {
						t.Fatalf("top at width %d, title %q, key %d: %d cells (%q)",
							width, title, key, got, join(b.Top(key, title)))
					}
					if got := cells(b.Bottom()); got != width {
						t.Fatalf("bottom at width %d: %d cells (%q)", width, got, b.Bottom())
					}
				}
			}
		}
	}
}

func kindName(k Kind) string {
	switch k {
	case KindBorder:
		return "border"
	case KindTitle:
		return "title"
	case KindHotkey:
		return "hotkey"
	}
	return "?"
}
