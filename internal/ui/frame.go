package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/ui/box"
)

// frameRows draws the same geometry for monitor panels and the help overlay.
// Every result is an exact rectangle, so composing it beside other content
// cannot move the rest of a row sideways.
func (m Model) frameRows(frame box.Box, title string, hotkey int, border lipgloss.Style, body []string) []string {
	// Three colours across one row of runes, which is why box.Top hands back
	// segments rather than a finished string: the border is the panel's own
	// colour, the name is the theme's title, and the digit that toggles this
	// panel is hi_fg -- a flat theme key looked up the same way title is,
	// not one of textAccents' ramp-indexed accents, since there is no ramp
	// here to index. Plain, not bold: alarm() also reaches for hi_fg, but
	// bold with it, for something wrong right now: a permanent hotkey
	// carries neither the weight nor the news, so it stops at the colour.
	var top strings.Builder
	for _, seg := range frame.Top(title, hotkey) {
		switch seg.Kind {
		case box.KindTitle:
			top.WriteString(m.title().Render(seg.Text))
		case box.KindHotkey:
			top.WriteString(m.style("hi_fg").Render(seg.Text))
		default:
			top.WriteString(border.Render(seg.Text))
		}
	}
	width, height := frame.Inner()
	side := border.Render(string(frame.Runes.Vertical))
	lines := make([]string, 0, frame.Height)
	lines = append(lines, top.String())
	for i := 0; i < height; i++ {
		row := ""
		if i < len(body) {
			row = body[i]
		}
		lines = append(lines, side+fitCell(row, width)+side)
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
