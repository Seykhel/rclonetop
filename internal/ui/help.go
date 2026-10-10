package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Seykhel/rclonetop/internal/ui/box"
)

// Rows carry the identity of their group or command through wrapping.
// Both rendering and dispatch use keyBindings; help is not a second keymap.
type helpLine struct {
	text  string
	entry int
}

func (m Model) helpLines(width int) []helpLine {
	var lines []helpLine
	entry, group := -1, ""
	appendEntry := func(text string, style lipgloss.Style) {
		entry++
		for _, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
			lines = append(lines, helpLine{style.Render(line), entry})
		}
	}
	appendEntry("Keyboard help", m.title())
	for _, binding := range keyBindings() {
		if binding.group != group {
			group = binding.group
			appendEntry(group, m.title())
		}
		appendEntry(binding.helpText(), m.value())
	}
	return lines
}

func helpWidth() int {
	width := max(lipgloss.Width("Keyboard help"), lipgloss.Width(helpHint))
	for _, binding := range keyBindings() {
		width = max(width, max(lipgloss.Width(binding.group), lipgloss.Width(binding.helpText())))
	}
	return width
}

const helpHint = "Esc / ? / h close  |  Up/Down PgUp/PgDn scroll"

func (m Model) helpFooter(width int) string {
	for _, hint := range []string{helpHint, "Esc / ? / h close", "Esc close", "Esc"} {
		if lipgloss.Width(hint) <= width {
			return m.label().Render(hint)
		}
	}
	return m.label().Render("?")
}

// The complete, unwrapped reference earns a frame only when it fits. Width
// and height are measured from its content, so a new command cannot leave the
// threshold silently too small for the help it is supposed to hold.
func (m Model) helpFits() bool {
	return helpWidth()+2 <= effectiveWidth(m.width) &&
		len(m.helpLines(helpWidth()))+2 <= effectiveHeight(m.height)
}

func (m *Model) scrollHelp(action keyAction) {
	if m.helpFits() {
		m.helpOffset = 0
		return
	}
	rows := max(effectiveHeight(m.height)-1, 0)
	switch action {
	case actionScrollUp:
		m.helpOffset--
	case actionScrollDown:
		m.helpOffset++
	case actionPageUp:
		m.helpOffset -= max(rows, 1)
	case actionPageDown:
		m.helpOffset += max(rows, 1)
	default:
		return
	}
	lines := m.helpLines(effectiveWidth(m.width))
	m.helpOffset = min(max(m.helpOffset, 0), max(len(lines)-rows, 0))
	m.helpAnchor = lines[min(m.helpOffset, len(lines)-1)].entry
}

func (m *Model) restoreHelpEntry(entry int) {
	m.helpOffset = 0
	if !m.helpFits() {
		for i, line := range m.helpLines(effectiveWidth(m.width)) {
			if line.entry == entry {
				m.helpOffset = i
				break
			}
		}
	}
	rows := max(effectiveHeight(m.height)-1, 0)
	m.helpOffset = min(m.helpOffset, max(len(m.helpLines(effectiveWidth(m.width)))-rows, 0))
}

func (m Model) renderHelp() string {
	if !m.helpFits() {
		width, height := effectiveWidth(m.width), effectiveHeight(m.height)
		body := m.helpLines(width)
		rows := make([]string, 0, height)
		for i := 0; i < height-1; i++ {
			line := ""
			if at := m.helpOffset + i; at < len(body) {
				line = body[at].text
			}
			rows = append(rows, fitCell(line, width))
		}
		rows = append(rows, fitCell(m.helpFooter(width), width))
		return strings.Join(rows, "\n")
	}

	inner := helpWidth()
	body := m.helpLines(inner)
	// The frame already carries the title. Full-screen help needs the title
	// as a content row, but drawing that row inside the frame repeats it.
	body = body[1:]
	frame := box.Box{Width: inner + 2, Height: len(body) + 3, Runes: m.boxRunes()}
	content := make([]string, 0, len(body)+1)
	for _, line := range body {
		content = append(content, line.text)
	}
	content = append(content, m.helpFooter(inner))
	panel := m.frameRows(frame, "Keyboard help", box.NoHotkey, m.style("div_line"), content)

	background := m.monitorView()
	if m.detailOpen {
		background = m.renderDetail()
	}
	return m.overlay(panel, frame.Width, background)
}
