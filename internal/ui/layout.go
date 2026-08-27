package ui

// This file decides where the framed view's panels go, and nothing else. It is
// arithmetic over two integers: no theme, no lipgloss, no state. That is
// deliberate and it is the same cut State.Resolve makes against the renderers --
// the interesting cases here are the awkward terminal sizes, and asserting them
// on placements rather than on rendered strings is what keeps a layout bug from
// arriving as a diff of escape sequences.

// defaultHeight is what the layout assumes before the terminal has reported its
// size, the conventional companion to defaultWidth.
const defaultHeight = 24

// effectiveHeight resolves the height the layout budgets against, on the same
// terms as effectiveWidth: a terminal can report zero, and every consumer has to
// answer that the same way or the view flips on the first size message.
func effectiveHeight(h int) int {
	if h <= 0 {
		return defaultHeight
	}
	return h
}

// denseBelow is the width under which the framed view is not worth drawing.
//
// A frame spends two of its columns on itself and the panels inside it lose two
// more each. At sixty columns that is still a tenth of the screen given to
// decoration; below it the figures start wrapping and the frame is buying
// nothing. The dense view is tuned for eighty and shrinks gracefully, so it is
// the honest answer for a narrow tmux pane rather than a degraded one.
const denseBelow = 60

// The rows the framed view spends outside the panels: the title line at the top,
// and the rule plus the sources line at the bottom. They are named separately
// because the placements are in screen coordinates, so a panel's first row is
// headerRows and the last row it may occupy is height-footerRows-1.
const (
	headerRows = 1
	footerRows = 2
	chromeRows = headerRows + footerRows
)

// panelKind names one panel. The four of them are the four kinds of question
// rclonetop answers, and they take the four box colours btop already spends on
// its own -- see the table in panels below.
type panelKind int

const (
	panelTransfers panelKind = iota
	panelBandwidth
	panelFiles
	panelStatus
)

func (k panelKind) String() string {
	if int(k) < len(panels) {
		return panels[k].title
	}
	return "?"
}

// panelSpec is what the layout knows about a panel: how small it may be, whether
// it can use more room than that, and the two strings the renderer needs to draw
// its top edge. The colour is a theme key rather than a colour, which is what
// keeps this file free of the theme.
//
// There is no hotkey here, and that is a decision rather than an omission. btop
// writes a digit beside each box title and the digit *does* something -- it
// toggles the box. Drawing one that does nothing is this repo's "a flag accepted
// and ignored is worse than one rejected", moved from the command line to the
// screen, where it is harder to notice and easier to believe. The digits come
// back with whatever selects a box, which is #7's shown_boxes.
type panelSpec struct {
	kind    panelKind
	title   string
	color   string
	minRows int
	// grows says the panel has a list or a graph in it that is worth more
	// rows. A panel that does not grow is drawn at its minimum and the
	// leftover goes to one that does -- btop's own arrangement, where the
	// process box takes what the fixed boxes leave.
	grows bool
}

// panels is the table, in the order they are read in: what is running, how fast
// it is going, which files, and whether anything is broken. The colours are the
// ones boxColorFor already assigns to the same kinds of work.
var panels = [...]panelSpec{
	panelTransfers: {panelTransfers, "transfers", "proc_box", 6, true},
	panelBandwidth: {panelBandwidth, "bandwidth", "net_box", 6, true},
	panelFiles:     {panelFiles, "files", "mem_box", 5, true},
	// Units, timers, caches and sync pairs: a handful of lines that do not
	// get better for being given more of them.
	panelStatus: {panelStatus, "status", "cpu_box", 5, false},
}

// placement is one panel's rectangle, in screen coordinates.
type placement struct {
	kind panelKind
	x, y int
	w, h int
}

// layout is the plan for one frame of the framed view.
type layout struct {
	// dense says the terminal cannot carry frames and the dense view should
	// be drawn instead. When it is true the rest of this is empty.
	dense bool

	// panels are the rectangles to draw, in the order they were placed.
	panels []placement

	// dropped are the panels there was no room for, in the order they were
	// given up. Nothing on screen shows them: the order they are surrendered
	// in is a decision -- see givingUpOrder -- and this is what makes that
	// decision assertable rather than inferable from which boxes happen to be
	// missing. Deriving it in the test instead would be the test recomputing
	// the rule it is checking.
	dropped []panelKind
}

// panelRows is how many rows of content each panel has to show, by kind.
//
// It is the one thing the layout knows about the data, and it is a count rather
// than the data itself: the file stays arithmetic over integers, which is what
// keeps the awkward sizes assertable without rendering anything.
//
// It exists because the first version distributed spare rows to whichever panels
// were marked as able to grow, which on a real host gave eleven rows to a
// transfers panel with two lines in it while status truncated to "+6 more"
// beside it. Room went to whoever could take it rather than to whoever had
// something to put in it.
type panelRows [len(panels)]int

// planLayout works out what fits in a terminal of this size. Width and height
// are taken raw, as the model holds them, and resolved here so that no caller
// has to remember that zero means "not yet reported". want may be the zero value,
// which means nobody has said -- every panel then sits at its minimum and the
// spare goes to the graphs, which is what this did before it was told.
// It escalates downwards: two columns, then one, then the dense view. Each step
// is tried and taken only if it comes out whole, which is what makes the height
// threshold derived rather than guessed -- an earlier attempt compared the height
// against a hand-written minimum, and a terminal seven rows tall passed it and
// then dropped every panel in turn, leaving a framed view with nothing in it.
// "Did anything survive" is the same question asked where the answer is known.
func planLayout(width, height int, want panelRows) layout {
	w, rows := effectiveWidth(width), effectiveHeight(height)-chromeRows

	if w >= twoColumnsFrom {
		if l, ok := planColumns(w, rows, want); ok {
			return l
		}
	}
	if w >= denseBelow {
		if keep, dropped := fit(readingOrder, rows); len(keep) > 0 {
			return layout{
				panels:  packColumn(keep, 0, headerRows, w, rows, want),
				dropped: dropped,
			}
		}
	}
	return layout{dense: true}
}

// planColumns is the two-column arrangement, and it reports whether it is worth
// having. A column that kept nothing is half a screen of nothing: the panels
// that survived are better off spread across the whole width, which is the
// arrangement one step down.
func planColumns(w, rows int, want panelRows) (layout, bool) {
	leftKeep, leftGone := fit(leftColumn, rows)
	rightKeep, rightGone := fit(rightColumn, rows)
	if len(leftKeep) == 0 || len(rightKeep) == 0 {
		return layout{}, false
	}

	lw := columnWidth(w)

	return layout{
		panels: append(
			packColumn(leftKeep, 0, headerRows, lw, rows, want),
			packColumn(rightKeep, lw, headerRows, w-lw, rows, want)...),
		dropped: append(leftGone, rightGone...),
	}, true
}

// columnWidth is how wide a panel's column is on a screen of this width.
//
// The odd column goes to the left, which is where the long strings are: remotes,
// mountpoints and file names, all of which are truncated from the middle and
// lose a syllable for every column they are short.
//
// One place answers this, because two would disagree by a column and the caller
// that guesses is the one measuring how much fits.
func columnWidth(width int) int {
	if width < twoColumnsFrom {
		return width
	}
	return (width + 1) / 2
}

// twoColumnsFrom is the width at which the screen is split in two.
//
// A hundred columns gives each side fifty, and a panel of fifty holds a
// truncated remote path and its figures without either of them wrapping. Below
// that the split buys nothing: two columns of thirty are two columns of
// ellipses, and the full-width stack says the same thing legibly.
const twoColumnsFrom = 100

// readingOrder is the order the panels are stacked in, top to bottom, when there
// is one column. The two-column split keeps that order within each column.
var (
	readingOrder = []panelKind{panelTransfers, panelBandwidth, panelFiles, panelStatus}
	// What is moving, and which files of it.
	leftColumn = []panelKind{panelTransfers, panelFiles}
	// How fast it is going, and whether anything is broken. Bandwidth is
	// here rather than under transfers because a graph is worth more width
	// than a list of names is, and because pairing it with status gives each
	// column something that grows -- see packColumn.
	rightColumn = []panelKind{panelBandwidth, panelStatus}
)

// givingUpOrder is the order they are kept in, most missed first, and it is
// deliberately not the reading order. Bandwidth is drawn above status because
// that is how the screen reads; status is kept ahead of bandwidth because a host
// running nothing has no bandwidth to report and the question it is being asked
// is whether last night's run succeeded. Files goes first: it is a detail of a
// transfer that the transfers panel already announces.
var givingUpOrder = []panelKind{panelTransfers, panelStatus, panelBandwidth, panelFiles}

// fit drops panels, whole, until the ones left fit the rows available.
//
// Whole is the point. A frame costs two rows before it says anything, so a panel
// squeezed to its last row spends more of the screen on its own border than on
// what it is for -- and the dense view, which this one replaced, said the same
// thing in one line. Giving the room back to the panels that remain is worth
// more than keeping a token of the one that went.
func fit(order []panelKind, rows int) (keep, dropped []panelKind) {
	need := 0
	for _, k := range order {
		need += panels[k].minRows
	}

	out := append([]panelKind(nil), order...)
	for i := len(givingUpOrder) - 1; i >= 0 && need > rows; i-- {
		victim := givingUpOrder[i]
		for j, k := range out {
			if k != victim {
				continue
			}
			out = append(out[:j], out[j+1:]...)
			need -= panels[victim].minRows
			dropped = append(dropped, victim)
			break
		}
	}
	return out, dropped
}

// packColumn stacks panels down one column and shares out the rows.
//
// Three passes, and the middle one is the whole point:
//
//  1. every panel takes its minimum;
//  2. a panel with more content than that grows to hold it, in the order they
//     are kept in, until the column runs out. This is what stops a list
//     truncating beside an empty box;
//  3. whatever nobody claimed is split evenly between the panels that grow --
//     the graphs, which fill any height they are given.
//
// The leftover in the third pass is shared evenly rather than piled onto the
// first panel: a transfers box twenty rows tall above a bandwidth box of six
// looks like a rendering fault. What remains after an even share goes to the
// panels earliest in the order, which is the same tie break the order encodes.
func packColumn(order []panelKind, x, top, w, rows int, want panelRows) []placement {
	if len(order) == 0 {
		return nil
	}

	out := make([]placement, 0, len(order))
	growers := 0
	used := 0
	for _, k := range order {
		out = append(out, placement{kind: k, x: x, w: w, h: panels[k].minRows})
		used += panels[k].minRows
		if panels[k].grows {
			growers++
		}
	}

	// Pass two: content before decoration.
	spare := rows - used
	for _, k := range givingUpOrder {
		if spare <= 0 {
			break
		}
		for i := range out {
			if out[i].kind != k {
				continue
			}
			// The frame's own two rows come on top of the content.
			need := want[k] + 2 - out[i].h
			if need > spare {
				need = spare
			}
			if need > 0 {
				out[i].h += need
				spare -= need
			}
		}
	}

	if spare > 0 {
		if growers == 0 {
			// Nothing here wants the room, and a column has to fill
			// its screen anyway: a gap above the footer reads as a
			// panel that failed to draw, not as one that was content
			// with its size. It happens when a short column has given
			// up the panels that grow -- status on its own, in the
			// right-hand column of a twelve-row terminal.
			out[len(out)-1].h += spare
		} else {
			each, extra := spare/growers, spare%growers
			for i := range out {
				if !panels[out[i].kind].grows {
					continue
				}
				out[i].h += each
				if extra > 0 {
					out[i].h++
					extra--
				}
			}
		}
	}

	y := top
	for i := range out {
		out[i].y = y
		y += out[i].h
	}
	return out
}
