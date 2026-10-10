package ui

import (
	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
	"time"
)

func selectionResult(m Model, snap model.Snapshot) Model {
	next, _ := m.Update(resultMsg(collect.Result{Snapshot: snap}))
	return next.(Model)
}
func selectionEnter(m Model) Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model)
}
func selectionFooter(m Model) string {
	lines := strings.Split(stripStyles(m.View()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
func TestSelectionFollowsTheSubjectRatherThanBusyOrder(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 100, 30)
	start := time.Unix(1787000000, 0)
	a := model.Process{PID: 11, Kind: model.KindCopy, StartedAt: start, ReadRate: 100, Paths: []string{"alpha"}}
	b := model.Process{PID: 22, Kind: model.KindCopy, StartedAt: start, ReadRate: 10, Paths: []string{"beta"}}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{a, b}})
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("selection must start inactive")
	}
	m, _ = helpKey(m, "down")
	if footer := selectionFooter(m); !strings.Contains(footer, "pid 11") || !strings.Contains(footer, "Enter details") {
		t.Fatalf("first subject not selected: %s", footer)
	}
	a.ReadRate = 1
	b.ReadRate = 100
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{a, b}})
	if !strings.Contains(selectionFooter(m), "pid 11") {
		t.Fatal("reordering changed subject")
	}
	m, _ = helpKey(m, "up")
	if !strings.Contains(selectionFooter(m), "pid 22") {
		t.Fatal("up should follow the new displayed sequence")
	}
	m, _ = helpKey(m, "up")
	if !strings.Contains(selectionFooter(m), "pid 22") {
		t.Fatal("navigation must not wrap")
	}
}

func TestDetailsShowCurrentEvidenceAndDoNotKeepDisappearedFacts(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 120, 50)
	start := time.Unix(1787000000, 0)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{{PID: 11, Kind: model.KindCopy, StartedAt: start, Paths: []string{"unique-remote:backup"}}}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	view := stripStyles(m.View())
	for _, want := range []string{"Subject details", "Identity", "pid 11", "unique-remote:backup", "throughput unavailable", "Statistics unavailable", "Sources"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in detail:\n%s", want, view)
		}
	}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{}})
	view = stripStyles(m.View())
	if !strings.Contains(view, "Subject no longer available") || strings.Contains(view, "unique-remote:backup") {
		t.Fatalf("disappearance retained obsolete facts:\n%s", view)
	}
	m, cmd := helpKey(m, "esc")
	if cmd != nil || strings.Contains(stripStyles(m.View()), "Subject details") {
		t.Fatal("Esc must return to monitor")
	}
}

func TestDetailScrollAndNestedHelpConsumeMonitorKeys(t *testing.T) {
	m := helpSize(New(nil, Options{VimKeys: true}, nil), 50, 7)
	start := time.Unix(1787000000, 0)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{{PID: 11, Kind: model.KindCopy, StartedAt: start, Paths: []string{"alpha"}}}})
	m, _ = helpKey(m, "j")
	m = selectionEnter(m)
	if !strings.Contains(stripStyles(m.View()), "Subject details") {
		t.Fatal("Vim j should select when enabled")
	}
	m, _ = helpKey(m, "pgdown")
	before := m.View()
	for _, key := range []string{"p", "P", "1", "+"} {
		m, _ = helpKey(m, key)
	}
	if m.View() != before {
		t.Fatal("monitor keys affected details")
	}
	m, _ = helpKey(m, "h")
	if !strings.Contains(stripStyles(m.View()), "Keyboard help") {
		t.Fatal("nested help failed")
	}
	m, _ = helpKey(m, "j")
	m, _ = helpKey(m, "esc")
	if m.View() != before {
		t.Fatal("nested help lost detail reading position")
	}
	m, _ = helpKey(m, "k")
	if m.View() == before {
		t.Fatal("Vim k should scroll detail")
	}
	m, _ = helpKey(m, "esc")
	m = selectionEnter(m)
	if !strings.Contains(stripStyles(m.View()), "Subject details") {
		t.Fatal("reopening should start at top")
	}
}

func TestSelectedFragmentsStayVisibleInBothViewsAndHiddenSubjectsReachDetails(t *testing.T) {
	for _, preset := range []int{0, 1} {
		m := helpSize(New(nil, Options{Preset: preset}, nil), 120, 40)
		start := time.Unix(1787000000, 0)
		m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{{PID: 11, Kind: model.KindCopy, StartedAt: start, IOAvailable: true}}})
		m = selectionResult(m, model.Snapshot{Source: model.SourceLog, At: start, Jobs: []model.Job{{PID: 11, Transferring: []model.Transfer{{Name: "file.txt"}}, Errors: []model.LogLine{{At: start, Message: "failed"}}}}})
		m, _ = helpKey(m, "down")
		count := strings.Count(stripStyles(m.View()), "> ")
		if count < 4 {
			t.Fatalf("preset %d: subject fragments need visible selection, got %d:\n%s", preset, count, stripStyles(m.View()))
		}
		if preset == 1 {
			for _, key := range []string{"1", "2", "3", "4"} {
				m, _ = helpKey(m, key)
			}
		}
		if !strings.Contains(selectionFooter(m), "pid 11") {
			t.Fatal("hidden panels hid selected identity")
		}
		m = selectionEnter(m)
		if !strings.Contains(stripStyles(m.View()), "Subject details") {
			t.Fatal("hidden subject details unreachable")
		}
	}
}
