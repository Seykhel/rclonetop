package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Seykhel/rclonetop/internal/model"
)

// The sequence comes from Resolve, before any panel hides or clips its rows.
// No row position or display label is retained across collector updates.
type selectableSubject struct {
	id      model.SubjectID
	process *model.ProcRow
	unit    *model.UnitRow
}

func subjects(v model.View) []selectableSubject {
	out := make([]selectableSubject, 0, len(v.Procs)+len(v.Units))
	for i := range v.Procs {
		r := &v.Procs[i]
		out = append(out, selectableSubject{id: r.Subject, process: r})
	}
	for i := range v.Units {
		r := &v.Units[i]
		out = append(out, selectableSubject{id: r.Subject, unit: r})
	}
	return out
}
func findSubject(v model.View, id model.SubjectID) (selectableSubject, bool) {
	if id == (model.SubjectID{}) {
		return selectableSubject{}, false
	}
	for _, s := range subjects(v) {
		if s.id == id {
			return s, true
		}
	}
	return selectableSubject{}, false
}
func (s selectableSubject) label() string {
	if s.id.Unit != "" {
		return s.id.Unit + " (" + s.id.Scope + ")"
	}
	if s.process != nil {
		return fmt.Sprintf("%s pid %d", s.process.Process.Kind, s.process.Process.PID)
	}
	return ""
}
func (m *Model) moveSelection(delta int) {
	list := subjects(m.state.Resolve())
	if len(list) == 0 {
		return
	}
	at := -1
	for i, s := range list {
		if s.id == m.selected {
			at = i
			break
		}
	}
	if at < 0 {
		if delta < 0 {
			at = len(list) - 1
		} else {
			at = 0
		}
	} else {
		at = min(max(at+delta, 0), len(list)-1)
	}
	m.selected = list[at].id
	m.selectedProcess = model.SubjectID{}
	if list[at].process != nil {
		m.selectedProcess = list[at].process.ProcessSubject
	}
}
func (m *Model) reconcileSelection() {
	v := m.state.Resolve()
	reconcile := func(id, alias model.SubjectID) (model.SubjectID, model.SubjectID) {
		if subject, ok := findSubject(v, id); ok {
			if subject.process != nil {
				alias = subject.process.ProcessSubject
			}
			return id, alias
		}
		if alias != (model.SubjectID{}) {
			for _, row := range v.Procs {
				if row.ProcessSubject == alias {
					return row.Subject, alias
				}
			}
		}
		return model.SubjectID{}, model.SubjectID{}
	}
	m.selected, m.selectedProcess = reconcile(m.selected, m.selectedProcess)
	if m.detailOpen {
		if id, alias := reconcile(m.detailSubject, m.detailProcess); id != (model.SubjectID{}) {
			m.detailSubject, m.detailProcess = id, alias
		}
	}
}

// A plain marker accompanies colour so console palettes can still identify
// every visible fragment. Replace its existing indent where possible.
func (m Model) markSubject(id model.SubjectID, text string, width int) string {
	if id != m.selected || id == (model.SubjectID{}) {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(stripStyles(line)) == "" {
			continue
		}
		if strings.HasPrefix(line, "  ") {
			line = line[2:]
		} else {
			line = ansi.Truncate(line, max(width-2, 0), "")
		}
		lines[i] = m.style("hi_fg").Render("> ") + line
	}
	return strings.Join(lines, "\n")
}
