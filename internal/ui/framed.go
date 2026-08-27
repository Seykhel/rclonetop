package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/box"
)

// renderFramed draws preset 1: btop's own arrangement, four framed panels over
// the width of the terminal.
//
// It renders nothing the dense view does not already render. The panels are the
// same fragments dealt out to four boxes, which is deliberate: the difference
// between the two views is where things are, and a fragment that only one of
// them can draw is a fragment only one of them gets tested.
//
// Every panel comes back as exactly its own rectangle -- p.h lines of p.w
// columns, padded -- because the columns are stitched row by row. A panel one
// column short slides everything to its right by one, and it does so on that row
// only, which reads as a broken frame rather than as a padding bug.
func (m Model) renderFramed() string {
	plan := planLayout(m.width, m.height)
	if plan.dense {
		return m.renderDense()
	}

	width := effectiveWidth(m.width)
	v := m.state.Resolve()

	lines := make([]string, 0, effectiveHeight(m.height))
	lines = append(lines, m.denseHeader(width))
	lines = append(lines, m.framedBody(plan, v)...)
	lines = append(lines, m.denseFooter(v.Seen, v.Errors, width))

	// The same final clamp the dense view applies, for the same reason and
	// with more at stake: a line that wraps here takes the frame with it.
	return lipgloss.NewStyle().MaxWidth(width).Render(strings.Join(lines, "\n"))
}

// framedBody draws the panels and stitches the columns together, one row at a
// time.
func (m Model) framedBody(plan layout, v model.View) []string {
	byX := map[int][]placement{}
	var xs []int
	for _, p := range plan.panels {
		if _, seen := byX[p.x]; !seen {
			xs = append(xs, p.x)
		}
		byX[p.x] = append(byX[p.x], p)
	}
	sort.Ints(xs)

	cols := make([][]string, 0, len(xs))
	rows := 0
	for _, x := range xs {
		var lines []string
		for _, p := range byX[x] {
			lines = append(lines, m.framedPanel(p, v)...)
		}
		if len(lines) > rows {
			rows = len(lines)
		}
		cols = append(cols, lines)
	}

	out := make([]string, rows)
	for i := range out {
		var b strings.Builder
		for c, lines := range cols {
			if i < len(lines) {
				b.WriteString(lines[i])
				continue
			}
			// The layout fills every column to the same depth, so this
			// is unreachable through planLayout. It is here because the
			// alternative to padding a short column is a ragged right
			// edge, and a renderer that cannot be given a bad plan is
			// one nobody has to reason about.
			b.WriteString(strings.Repeat(" ", byX[xs[c]][0].w))
		}
		out[i] = b.String()
	}
	return out
}

// framedPanel draws one panel: its top edge with the title in it, its content
// padded to the inner width, and its bottom edge.
func (m Model) framedPanel(p placement, v model.View) []string {
	spec := panels[p.kind]
	frame := box.Box{Width: p.w, Height: p.h, Runes: m.boxRunes()}
	border := m.style(spec.color)

	// Three colours across one row of runes, which is why box.Top hands back
	// segments rather than a finished string: the border is the panel's own
	// colour, the name is the theme's title, and the digit that selects it is
	// hi_fg -- btop's arrangement exactly.
	var top strings.Builder
	for _, seg := range frame.Top(spec.title, spec.hotkey) {
		switch seg.Kind {
		case box.KindTitle:
			top.WriteString(m.style("title").Bold(true).Render(seg.Text))
		case box.KindHotkey:
			top.WriteString(m.style("hi_fg").Bold(true).Render(seg.Text))
		default:
			top.WriteString(border.Render(seg.Text))
		}
	}

	innerW, innerH := frame.Inner()
	body := m.panelBody(p.kind, v, innerW, innerH)
	side := border.Render(string(frame.Runes.Vertical))

	lines := make([]string, 0, p.h)
	lines = append(lines, top.String())
	for i := 0; i < innerH; i++ {
		row := ""
		if i < len(body) {
			row = body[i]
		}
		lines = append(lines, side+fitCell(row, innerW)+side)
	}
	return append(lines, border.Render(frame.Bottom()))
}

// fitCell cuts a rendered line to the room it has and pads what is left, so a
// panel is a rectangle whatever is written in it. lipgloss does both without
// breaking the escape sequences inside.
func fitCell(s string, width int) string {
	s = lipgloss.NewStyle().MaxWidth(width).Render(s)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// panelBody is what goes inside one panel, already cut to the rows available.
func (m Model) panelBody(k panelKind, v model.View, width, height int) []string {
	var lines []string
	switch k {
	case panelTransfers:
		lines = m.transfersBody(v, width)
	case panelBandwidth:
		lines = m.bandwidthBody(v, width)
	case panelFiles:
		lines = m.filesBody(v, width)
	case panelStatus:
		lines = m.statusBody(v, width)
	}
	return m.clip(lines, height)
}

// clip cuts a body to the rows a panel has, and says how many lines went.
//
// The count is the same rule the files-in-flight list already follows: four rows
// and no count read as a job with four files left in it. A panel that silently
// stops at the frame is worse, because the frame looks like the end of the list.
func (m Model) clip(lines []string, height int) []string {
	if len(lines) <= height {
		return lines
	}
	// A panel of no rows takes no lines, rather than panicking on the
	// subtraction. Unreachable through planLayout, whose smallest panel keeps
	// three inner rows -- and guarded here on the same grounds as the short
	// column in framedBody: a renderer that cannot be given a bad plan is one
	// nobody has to reason about.
	if height < 1 {
		return nil
	}
	out := append([]string(nil), lines[:height-1]...)
	return append(out, m.label().Render(fmt.Sprintf("+%d more", len(lines)-height+1)))
}

// bodyLines splits a rendered fragment into rows, dropping the blank one a
// fragment ends with. The dense view writes those deliberately -- they separate
// the sections of a stacked screen -- and inside a frame they are rows spent on
// nothing.
func bodyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(stripStyles(line)) != "" {
			out = append(out, line)
		}
	}
	return out
}

// stripStyles takes the escape sequences back out of a rendered string.
//
// It is bodyLines' emptiness test -- a styled empty string is a pair of escapes
// around nothing, which TrimSpace alone reports as content -- and it is what the
// tests read a rendered view with. One copy, in production, because the two
// halves of a duplicate diverge at the next terminator somebody adds, and the
// half that would go stale is the one the assertions run through.
func stripStyles(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r == 'm' || r == 'K'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// transfersBody is what is running and what it costs.
func (m Model) transfersBody(v model.View, width int) []string {
	switch {
	case len(v.Seen) == 0:
		// Before any collector has reported, "nothing is running" would
		// be a claim rclonetop has not yet checked.
		return []string{m.style("inactive_fg").Render("collecting…")}
	case len(v.Procs) == 0:
		return []string{m.style("inactive_fg").Render("no rclone process running")}
	}

	var lines []string
	for _, row := range v.Procs {
		lines = append(lines, m.procHead(row.Process, width), m.procMeta(row.Process))
		lines = append(lines, bodyLines(m.jobProgress(row.Job))...)
		if frac, ok := row.Job.Stats.Done(); ok {
			// The bar the dense view has no room for, under the
			// figures it restates. Graded along "cpu", the same ramp
			// jobProgress paints its percentage with, so the two read
			// as one statement rather than two.
			//
			// Only when the completion is known: Done() is false
			// without a total, and a bar at nought per cent would be a
			// claim about progress rather than a report of it.
			lines = append(lines, "  "+m.meter("cpu", frac, width-meterMargin))
		}
	}
	return lines
}

// meterMargin is the two spaces a bar is indented by and the two it leaves at
// the end, so it lines up under the figures above it instead of touching the
// frame on either side.
const meterMargin = 4

// bandwidthBody is how fast it is going, one line per process. The width is the
// panel's own: the graphs are budgeted against the room this line has, which
// inside a frame is not the terminal.
func (m Model) bandwidthBody(v model.View, width int) []string {
	if len(v.Procs) == 0 {
		return []string{m.style("inactive_fg").Render("idle")}
	}
	var lines []string
	for _, row := range v.Procs {
		lines = append(lines, m.procThroughput(row.Process, width))
	}
	return lines
}

// filesBody is which files are moving right now.
func (m Model) filesBody(v model.View, width int) []string {
	var lines []string
	for _, row := range v.Procs {
		lines = append(lines, bodyLines(m.filesInFlight(row.Job, width))...)
	}
	if len(lines) == 0 {
		// Nil and empty mean different things about a job's file list,
		// and both of them mean there is nothing to draw here.
		return []string{m.style("inactive_fg").Render("no files in flight")}
	}
	return lines
}

// statusBody is everything that is not a running transfer: the units, the mounts
// nobody is serving, the bisync pairs, the caches, and whatever went wrong.
func (m Model) statusBody(v model.View, width int) []string {
	var lines []string
	for _, mnt := range v.Orphans {
		lines = append(lines, bodyLines(m.denseOrphanMount(mnt, width))...)
	}
	for _, pair := range v.Pairs {
		lines = append(lines, bodyLines(m.denseSyncPair(pair, width))...)
	}
	lines = append(lines, bodyLines(m.denseUnits(v.Units, width))...)
	lines = append(lines, bodyLines(m.denseCaches(v.Caches))...)
	for _, row := range v.Procs {
		lines = append(lines, bodyLines(m.renderErrors(row.Errors, width))...)
	}
	if len(lines) == 0 {
		return []string{m.style("inactive_fg").Render("nothing to report")}
	}
	return lines
}
