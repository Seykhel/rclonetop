package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Seykhel/rclonetop/internal/ui/box"
)

// Help entries have stable identities across wrapping. A resize can then keep
// the command being read on screen rather than retaining a row number that now
// belongs to a different command.
type helpEntry struct {
	text    string
	heading bool
}

var helpEntries = []helpEntry{
	{"Keyboard help", true},
	{"Help", true},
	{"? / h          Open or close help", false},
	{"Esc            Close help; outside help, quit", false},
	{"Up / Down      Scroll help by one line", false},
	{"PgUp / PgDn    Scroll help by one page", false},
	{"Views", true},
	{"p              Alternate dense and remembered framed view", false},
	{"P              Cycle configured framed presets (enter framed from dense)", false},
	{"Framed panels (session only)", true},
	{"1              Toggle transfers", false},
	{"2              Toggle bandwidth", false},
	{"3              Toggle files", false},
	{"4              Toggle status", false},
	{"Refresh (screen only; collector cadence stays the same)", true},
	{"+ / =          Refresh faster (halve interval, minimum 100ms)", false},
	{"- / _          Refresh slower (double interval, maximum 30000ms)", false},
	{"Quit", true},
	{"q / Ctrl+C     Quit, including while help is open", false},
}

type helpLine struct {
	text  string
	entry int
}

func (m Model) helpLines(width int) []helpLine {
	var lines []helpLine
	for i, entry := range helpEntries {
		style := m.value()
		if entry.heading {
			style = m.style("title").Bold(true)
		}
		for _, text := range strings.Split(ansi.Wrap(entry.text, width, ""), "\n") {
			lines = append(lines, helpLine{style.Render(text), i})
		}
	}
	return lines
}

func helpWidth() int {
	width := 0
	for _, entry := range helpEntries {
		width = max(width, lipgloss.Width(entry.text))
	}
	return max(width, lipgloss.Width(helpHint))
}

const helpHint = "Esc / ? / h close  |  Up/Down PgUp/PgDn scroll"

func (m Model) helpFooter(width int) string {
	for _, hint := range []string{helpHint, "Esc / ? / h close", "Esc close", "Esc"} {
		if lipgloss.Width(hint) <= width {
			return m.label().Render(hint)
		}
	}
	return m.label().Render(ansi.Truncate("Esc", width, ""))
}

// The complete, unwrapped reference earns a frame only when it fits. Width
// and height are measured from its content, so a new command cannot leave the
// threshold silently too small for the help it is supposed to hold.
func (m Model) helpFits() bool {
	return helpWidth()+2 <= effectiveWidth(m.width) &&
		len(m.helpLines(helpWidth()))+3 <= effectiveHeight(m.height)
}

func (m *Model) scrollHelp(key string) {
	if m.helpFits() {
		m.helpOffset = 0
		return
	}
	rows := max(effectiveHeight(m.height)-1, 0)
	switch key {
	case "up":
		m.helpOffset--
	case "down":
		m.helpOffset++
	case "pgup":
		m.helpOffset -= max(rows, 1)
	case "pgdown":
		m.helpOffset += max(rows, 1)
	}
	m.helpOffset = min(max(m.helpOffset, 0), max(len(m.helpLines(effectiveWidth(m.width)))-rows, 0))
}

func (m Model) helpEntryAtTop() int {
	if !m.helpOpen || m.helpFits() {
		return 0
	}
	lines := m.helpLines(effectiveWidth(m.width))
	return lines[min(m.helpOffset, len(lines)-1)].entry
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
	m.scrollHelp("")
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

	width, height := effectiveWidth(m.width), effectiveHeight(m.height)
	inner := helpWidth()
	body := m.helpLines(inner)
	frame := box.Box{Width: inner + 2, Height: len(body) + 3, Runes: m.boxRunes()}
	border := m.style("div_line")
	var top strings.Builder
	for _, seg := range frame.Top("Keyboard help", box.NoHotkey) {
		style := border
		if seg.Kind == box.KindTitle {
			style = m.style("title").Bold(true)
		}
		top.WriteString(style.Render(seg.Text))
	}
	panel := []string{top.String()}
	side := border.Render(string(frame.Runes.Vertical))
	for _, line := range body {
		panel = append(panel, side+fitCell(line.text, inner)+side)
	}
	panel = append(panel, side+fitCell(m.helpFooter(inner), inner)+side, border.Render(frame.Bottom()))

	// Slice the monitor in terminal cells, preserving its styles on either
	// side. Dense content can exceed the screen's height; only the visible
	// rectangle belongs behind an overlay.
	background := strings.Split(m.monitorView(), "\n")
	rows := make([]string, height)
	x, y := (width-frame.Width)/2, (height-frame.Height)/2
	for i := range rows {
		line := ""
		if i < len(background) {
			line = background[i]
		}
		rows[i] = fitCell(line, width)
		if i >= y && i < y+len(panel) {
			rows[i] = ansi.Cut(rows[i], 0, x) +
				lipgloss.NewStyle().Background(m.opts.Theme.Color("main_bg").Lipgloss()).Render(panel[i-y]) +
				ansi.Cut(rows[i], x+frame.Width, width)
		}
	}
	return strings.Join(rows, "\n")
}
