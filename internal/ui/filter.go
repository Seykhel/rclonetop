package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/Seykhel/rclonetop/internal/model"
)

func (m Model) currentFilter() string {
	if m.filterEditing {
		return strings.TrimSpace(m.filterBuffer)
	}
	return m.filterQuery
}
func (m Model) filterActive() bool            { return m.currentFilter() != "" }
func (m Model) monitorProjection() model.View { return m.state.Resolve().Filter(m.currentFilter()) }
func (m Model) filteredEmpty() string {
	if len(subjects(m.monitorProjection())) == 0 {
		return "no matching subjects"
	}
	return "no matching process"
}
func (m *Model) clearExcludedSelection() {
	if _, ok := findSubject(m.monitorProjection(), m.selected); !ok {
		m.selected = model.SubjectID{}
		m.selectedProcess = model.SubjectID{}
	}
}
func (m *Model) beginFilter() {
	m.filterEditing = true
	m.filterBuffer = m.filterQuery
	m.filterCursor = len(m.filterBuffer)
	m.filterSaved = m.selected
	m.filterSavedProcess = m.selectedProcess
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC && !msg.Paste {
		m.quitting = true
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}
	if msg.Paste || (msg.Type == tea.KeyRunes && !msg.Alt) || msg.Type == tea.KeySpace {
		text := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			text = " "
		}
		text = strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, text)
		if text == "" {
			return m, nil
		}
		m.filterBuffer = m.filterBuffer[:m.filterCursor] + text + m.filterBuffer[m.filterCursor:]
		m.filterCursor += len(text)
		// A combining mark may join its neighbours; keep the insertion cursor at a whole grapheme boundary.
		m.filterCursor = nextBoundary(m.filterBuffer, m.filterCursor-1)
	} else {
		switch msg.Type {
		case tea.KeyEnter:
			m.filterQuery = strings.TrimSpace(m.filterBuffer)
			m.filterEditing = false
		case tea.KeyEsc:
			m.filterEditing = false
			m.selected = m.filterSaved
			m.selectedProcess = m.filterSavedProcess
		case tea.KeyLeft:
			m.filterCursor = previousBoundary(m.filterBuffer, m.filterCursor)
		case tea.KeyRight:
			m.filterCursor = nextBoundary(m.filterBuffer, m.filterCursor)
		case tea.KeyHome:
			m.filterCursor = 0
		case tea.KeyEnd:
			m.filterCursor = len(m.filterBuffer)
		case tea.KeyBackspace, tea.KeyCtrlH:
			at := previousBoundary(m.filterBuffer, m.filterCursor)
			m.filterBuffer = m.filterBuffer[:at] + m.filterBuffer[m.filterCursor:]
			m.filterCursor = boundaryAtOrAfter(m.filterBuffer, at)
		case tea.KeyDelete:
			end := nextBoundary(m.filterBuffer, m.filterCursor)
			m.filterBuffer = m.filterBuffer[:m.filterCursor] + m.filterBuffer[end:]
			m.filterCursor = boundaryAtOrAfter(m.filterBuffer, m.filterCursor)
		case tea.KeyCtrlU:
			m.filterBuffer = ""
			m.filterCursor = 0
		}
	}
	m.clearExcludedSelection()
	return m, nil
}

// Deleting a separator can join the surrounding runes into one grapheme.
// Move to its end rather than leave the next edit inside that character.
func boundaryAtOrAfter(s string, at int) int {
	if at == 0 {
		return 0
	}
	return nextBoundary(s, at-1)
}

func previousBoundary(s string, at int) int {
	last := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		_, end := g.Positions()
		if end >= at {
			return last
		}
		last = end
	}
	return last
}
func nextBoundary(s string, at int) int {
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		_, end := g.Positions()
		if end > at {
			return end
		}
	}
	return len(s)
}

func (m Model) filterStatus(width int) string {
	full := m.state.Resolve()
	matched := full.Filter(m.currentFilter())
	count := fmt.Sprintf(" %d/%d", len(subjects(matched)), len(subjects(full)))
	prefix := "filter: "
	query := m.currentFilter()
	if width < len(prefix) {
		return m.value().Render(ansi.Truncate("filter", width, ""))
	}
	room := width - len(prefix)
	if room >= len(count)+1 {
		query = ansi.Truncate(query, room-len(count), "…")
		return m.value().Render(prefix + query + count)
	}
	return m.value().Render(prefix + ansi.Truncate(query, room, "…"))
}
func (m Model) filterPrompt(width int) string {
	if width <= 0 {
		return ""
	}
	prefix := "/ "
	if width < 3 {
		prefix = ""
	}
	room := width - len(prefix)
	before := m.filterBuffer[:m.filterCursor]
	after := m.filterBuffer[m.filterCursor:]
	// Reserve one cell for a cursor, retaining the text immediately before it.
	for uniseg.StringWidth(before) > room-1 {
		before = before[nextBoundary(before, 0):]
	}
	cursor := " "
	tail := ""
	if after != "" {
		end := nextBoundary(after, 0)
		cursor = after[:end]
		tail = after[end:]
	}
	if uniseg.StringWidth(cursor) > room-uniseg.StringWidth(before) {
		cursor = " "
	}
	remaining := room - uniseg.StringWidth(before) - uniseg.StringWidth(cursor)
	return m.value().Render(prefix+before) + m.value().Reverse(true).Render(cursor) + m.value().Render(ansi.Truncate(tail, remaining, ""))
}
