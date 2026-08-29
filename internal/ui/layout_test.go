package ui

import "testing"

// A frame costs two columns and two rows of every panel it draws, and on a small
// terminal that is most of what there is. The dense view is what rclonetop shows
// instead -- it is tuned for eighty columns and degrades to a tmux side pane --
// so the layout's first answer is whether to draw itself at all.
func TestATerminalWithNoRoomForFramesAsksForTheDenseView(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		want          bool
	}{
		{"a narrow side pane", 40, 40, true},
		{"one column short of the threshold", 59, 40, true},
		{"a shell two rows tall", 120, 2, true},
		{"wide enough and tall enough", 120, 40, false},
		{"the conventional eighty by twenty-four", 80, 24, false},
		// Nothing reported yet: the same default every other consumer
		// resolves an unreported size to, or the view flips on the first
		// WindowSizeMsg for no reason the user can see.
		{"before the terminal has said anything", 0, 0, false},
	}
	for _, c := range cases {
		if got := planLayout(c.width, c.height, panelRows{}, allShown()).dense; got != c.want {
			t.Errorf("%s (%dx%d): dense = %v, want %v", c.name, c.width, c.height, got, c.want)
		}
	}
}

// kinds is the order the panels came out in, which is what most of these
// assertions are really about.
func kinds(l layout) []panelKind {
	out := make([]panelKind, 0, len(l.panels))
	for _, p := range l.panels {
		out = append(out, p.kind)
	}
	return out
}

func same(a, b []panelKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Below the two-column width every panel is the full width of the terminal,
// stacked in the order they are read in: what is running, how fast it is going,
// which files, and whether anything is broken.
func TestOneColumnStacksEveryPanelFullWidth(t *testing.T) {
	const width, height = 80, 30
	l := planLayout(width, height, panelRows{}, allShown())

	want := []panelKind{panelTransfers, panelBandwidth, panelFiles, panelStatus}
	if got := kinds(l); !same(got, want) {
		t.Fatalf("panels = %v, want %v", got, want)
	}

	y := headerRows
	for _, p := range l.panels {
		if p.x != 0 || p.w != width {
			t.Errorf("%v spans %d..%d, want the whole width", p.kind, p.x, p.x+p.w)
		}
		if p.y != y {
			t.Errorf("%v starts at row %d, want %d -- a gap or an overlap", p.kind, p.y, y)
		}
		y += p.h
	}

	// The panels fill the rows the chrome leaves, exactly. Leftover rows at
	// the bottom of a framed view read as a rendering fault, and a row too
	// many pushes the footer off the screen.
	if want := height - footerRows; y != want {
		t.Errorf("the panels end at row %d, want %d", y, want)
	}
}

// The rule that decides what a short terminal loses. A squeezed panel is a
// frame with one row of content in it, which says less than the dense line it
// replaced and costs two rows to say it; so the panel goes, whole, and the ones
// that stay keep their room.
func TestAPanelThatDoesNotFitIsDroppedWhole(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		want          []panelKind
		gone          []panelKind
	}{
		// Twenty-one rows for panels needing twenty-two: the first to go
		// is the one whose absence is least felt.
		{"eighty by twenty-four", 80, 24, []panelKind{panelTransfers, panelBandwidth, panelStatus}, []panelKind{panelFiles}},
		// Room for one panel only, and it is the one the program is for.
		{"a twelve row shell", 80, 12, []panelKind{panelTransfers}, []panelKind{panelFiles, panelBandwidth, panelStatus}},
	}
	for _, c := range cases {
		l := planLayout(c.width, c.height, panelRows{}, allShown())
		if got := kinds(l); !same(got, c.want) {
			t.Errorf("%s: panels = %v, want %v", c.name, got, c.want)
		}
		if !same(l.dropped, c.gone) {
			t.Errorf("%s: dropped = %v, want %v", c.name, l.dropped, c.gone)
		}
		for _, p := range l.panels {
			if p.h < panels[p.kind].minRows {
				t.Errorf("%s: %v was squeezed to %d rows, its minimum is %d",
					c.name, p.kind, p.h, panels[p.kind].minRows)
			}
		}
		// Whatever survived still fills the rows the chrome leaves.
		if bottom := l.panels[len(l.panels)-1].y + l.panels[len(l.panels)-1].h; bottom != c.height-footerRows {
			t.Errorf("%s: the panels end at row %d, want %d", c.name, bottom, c.height-footerRows)
		}
	}
}

// Above the threshold the screen is split in two, because a hundred and forty
// columns of full-width panels is a lot of white space to the right of every
// figure -- the complaint #11 opens with. The split is by subject: what is
// moving on the left, how it is going and whether it is healthy on the right.
func TestAWideTerminalSplitsIntoTwoColumns(t *testing.T) {
	const width, height = 140, 40
	l := planLayout(width, height, panelRows{}, allShown())

	left, right := map[panelKind]placement{}, map[panelKind]placement{}
	for _, p := range l.panels {
		if p.x == 0 {
			left[p.kind] = p
		} else {
			right[p.kind] = p
		}
	}

	for _, k := range []panelKind{panelTransfers, panelFiles} {
		if _, ok := left[k]; !ok {
			t.Errorf("%v is not in the left column", k)
		}
	}
	for _, k := range []panelKind{panelBandwidth, panelStatus} {
		if _, ok := right[k]; !ok {
			t.Errorf("%v is not in the right column", k)
		}
	}

	// The two columns meet, with no seam and no overlap, and together they
	// are the terminal.
	lw, rx, rw := left[panelTransfers].w, right[panelBandwidth].x, right[panelBandwidth].w
	if rx != lw || lw+rw != width {
		t.Errorf("columns are 0..%d and %d..%d, want them to meet and end at %d", lw, rx, rx+rw, width)
	}
	// Neither column is a sliver: an eighty-twenty split would put a file
	// name in a column that cannot hold one.
	if lw < width/3 || rw < width/3 {
		t.Errorf("columns %d and %d are lopsided", lw, rw)
	}

	// Both columns start under the header and both reach the footer. A short
	// column leaves a hole at the bottom of the screen, which reads as a
	// panel that failed to draw.
	for _, col := range []map[panelKind]placement{left, right} {
		top, bottom := height, 0
		for _, p := range col {
			if p.y < top {
				top = p.y
			}
			if p.y+p.h > bottom {
				bottom = p.y + p.h
			}
		}
		if top != headerRows || bottom != height-footerRows {
			t.Errorf("a column runs %d..%d, want %d..%d", top, bottom, headerRows, height-footerRows)
		}
	}
}

// The threshold itself, from both sides.
func TestTheSecondColumnArrivesAtItsOwnWidth(t *testing.T) {
	for _, c := range []struct {
		width int
		want  int
	}{
		{twoColumnsFrom - 1, 1},
		{twoColumnsFrom, 2},
	} {
		columns := map[int]bool{}
		for _, p := range planLayout(c.width, 40, panelRows{}, allShown()).panels {
			columns[p.x] = true
		}
		if len(columns) != c.want {
			t.Errorf("%d columns wide gave %d columns, want %d", c.width, len(columns), c.want)
		}
	}
}

// The sweep #11 asks for, as properties rather than as expected rectangles: at
// every size the plan covers the screen once. Whatever the arithmetic does with
// an odd number of rows or a column that lost its only growing panel, it may not
// leave a hole and it may not draw two panels over each other.
func TestEverySizeIsCoveredExactlyOnce(t *testing.T) {
	for _, width := range []int{10, 15, 24, 40, 60, 61, 80, 99, 100, 120, 190} {
		for _, height := range []int{3, 7, 8, 12, 24, 25, 40, 60} {
			l := planLayout(width, height, panelRows{}, allShown())
			if l.dense {
				if len(l.panels) != 0 {
					t.Errorf("%dx%d: the dense fallback still placed %d panels", width, height, len(l.panels))
				}
				continue
			}
			if len(l.panels) == 0 {
				t.Errorf("%dx%d: framed, and nothing in it", width, height)
				continue
			}

			// One cell of the panel area, one panel. The area is every
			// row between the header and the footer, across the whole
			// width.
			w, h := effectiveWidth(width), effectiveHeight(height)
			owner := make(map[[2]int]panelKind, w*h)
			for _, p := range l.panels {
				if p.w <= 0 || p.h <= 0 {
					t.Errorf("%dx%d: %v is %dx%d", width, height, p.kind, p.w, p.h)
				}
				for y := p.y; y < p.y+p.h; y++ {
					for x := p.x; x < p.x+p.w; x++ {
						if prev, taken := owner[[2]int{x, y}]; taken {
							t.Fatalf("%dx%d: %v and %v both claim %d,%d",
								width, height, prev, p.kind, x, y)
						}
						owner[[2]int{x, y}] = p.kind
					}
				}
			}
			for y := headerRows; y < h-footerRows; y++ {
				for x := 0; x < w; x++ {
					if _, taken := owner[[2]int{x, y}]; !taken {
						t.Fatalf("%dx%d: nothing covers %d,%d", width, height, x, y)
					}
				}
			}
		}
	}
}

// The screenshot that sent this back: status truncating to "+6 more" beside a
// transfers panel showing two lines in eleven. Space was going to whoever was
// marked as able to use it rather than to whoever had something to put in it.
//
// A panel that says how many rows it has gets them, up to what the column can
// give, before a graph takes the rest.
func TestAPanelGetsTheRowsItSaysItHas(t *testing.T) {
	const width, height = 120, 32
	rows := height - chromeRows

	var want panelRows
	want[panelStatus] = 9 // a unit, a timer, a sync pair, some caches
	l := planLayout(width, height, want, allShown())

	got := map[panelKind]placement{}
	for _, p := range l.panels {
		got[p.kind] = p
	}

	// Nine rows of content plus the two the frame takes.
	if h := got[panelStatus].h; h < 11 {
		t.Errorf("status has nine rows to show and was given %d, room for %d", h, h-2)
	}
	// And its column still fills the screen: what status did not want went to
	// the graph beside it, which uses any height it is given.
	if a, b := got[panelStatus].h, got[panelBandwidth].h; a+b != rows {
		t.Errorf("the right column is %d rows, want %d", a+b, rows)
	}

	// A panel that wants nothing does not shrink below its minimum: the
	// figures it does have still need room, and an empty box is honest about
	// a host with nothing running.
	if h := got[panelTransfers].h; h < panels[panelTransfers].minRows {
		t.Errorf("transfers was squeezed to %d rows, its minimum is %d", h, panels[panelTransfers].minRows)
	}

	// Asking for more than the column holds is not an error, it is a "+N
	// more": the demand is a claim about content, not a reservation.
	huge := planLayout(width, height, panelRows{panelStatus: 500}, allShown())
	for _, p := range huge.panels {
		if p.h > rows {
			t.Errorf("%v claimed %d rows of a %d-row column", p.kind, p.h, rows)
		}
	}
}

// A panel outside shown_boxes is a third reason for the same nothing on
// screen, and it has to stay a third reason: neither placed, nor counted
// among the ones fit gave up for lack of room -- that list is for a panel
// that wanted to be there and did not fit, not one that was never asked.
func TestAHiddenPanelIsNeitherPlacedNorDropped(t *testing.T) {
	const width, height = 80, 30
	sh := allShown()
	sh[panelFiles] = false

	l := planLayout(width, height, panelRows{}, sh)

	want := []panelKind{panelTransfers, panelBandwidth, panelStatus}
	if got := kinds(l); !same(got, want) {
		t.Fatalf("panels = %v, want %v", got, want)
	}
	if len(l.dropped) != 0 {
		t.Errorf("dropped = %v, want none -- files was hidden, not squeezed out", l.dropped)
	}

	// The room files would have taken went to the panels that remain,
	// exactly as it would have if fit had dropped it for lack of space.
	y := headerRows
	for _, p := range l.panels {
		y += p.h
	}
	if want := height - footerRows; y != want {
		t.Errorf("the panels end at row %d, want %d -- the freed room was not repacked", y, want)
	}
}

// Every panel hidden is the same question planLayout already answers for a
// terminal too narrow for any of them: did anything survive. Answered the
// same way, with no new branch.
func TestAnEmptyShownSetIsTheDenseView(t *testing.T) {
	l := planLayout(120, 40, panelRows{}, panelSet{})
	if !l.dense {
		t.Error("every panel hidden should fall back to the dense view")
	}
	if len(l.panels) != 0 {
		t.Errorf("the dense fallback still placed %d panels", len(l.panels))
	}
}

func TestParseShownBoxesEmptyMeansEverything(t *testing.T) {
	if got, want := parseShownBoxes(""), allShown(); got != want {
		t.Errorf("parseShownBoxes(\"\") = %v, want %v", got, want)
	}
}

// internal/config passes shown_boxes through unvalidated; here is where an
// unrecognised name is actually handled, by dropping it rather than
// refusing to start.
func TestParseShownBoxesDropsAnUnrecognisedName(t *testing.T) {
	got := parseShownBoxes("transfers remotes status")
	want := panelSet{panelTransfers: true, panelStatus: true}
	if got != want {
		t.Errorf("parseShownBoxes = %v, want %v", got, want)
	}
}

// panelSet is membership, not a sequence: what order the names arrived in,
// and how many times, says nothing planLayout ever asks.
func TestParseShownBoxesIgnoresOrderAndRepeats(t *testing.T) {
	got := parseShownBoxes("status status transfers")
	want := panelSet{panelTransfers: true, panelStatus: true}
	if got != want {
		t.Errorf("parseShownBoxes = %v, want %v", got, want)
	}
}

// The literal mapping #23 asks for, pinned against the numbers rather than
// against panels[k].hotkey itself -- a test that read the table back at
// itself would pass just as well with every digit renumbered.
func TestPanelHotkeysAreFixedInReadingOrder(t *testing.T) {
	want := map[panelKind]int{
		panelTransfers: 1,
		panelBandwidth: 2,
		panelFiles:     3,
		panelStatus:    4,
	}
	for k, hotkey := range want {
		if got := panels[k].hotkey; got != hotkey {
			t.Errorf("%v hotkey = %d, want %d", k, got, hotkey)
		}
	}
}

// panelForHotkey is candidates' mirror image: given the digit, name the
// panel. A digit with no panel -- 0, or anything outside 1-4 -- names none.
func TestPanelForHotkeyMatchesTheTable(t *testing.T) {
	for _, k := range []panelKind{panelTransfers, panelBandwidth, panelFiles, panelStatus} {
		got, ok := panelForHotkey(panels[k].hotkey)
		if !ok || got != k {
			t.Errorf("panelForHotkey(%d) = %v, %v; want %v, true", panels[k].hotkey, got, ok, k)
		}
	}
	if _, ok := panelForHotkey(0); ok {
		t.Error("panelForHotkey(0) should name no panel")
	}
}
