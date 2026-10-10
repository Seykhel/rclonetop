package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
)

func filterKey(m Model, typ tea.KeyType) Model {
	n, _ := m.Update(tea.KeyMsg{Type: typ})
	return n.(Model)
}
func filterText(m Model, text string) Model {
	n, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return n.(Model)
}
func filterFixture(preset int) Model {
	m := helpSize(New(nil, Options{Preset: preset, VimKeys: true}, nil), 120, 35)
	return selectionResult(m, model.Snapshot{Source: model.SourceProc, At: time.Now(), Processes: []model.Process{
		{PID: 11, Kind: model.KindCopy, StartedAt: time.Unix(100, 0), Paths: []string{"alpha:photos"}},
		{PID: 22, Kind: model.KindSync, StartedAt: time.Unix(100, 0), Paths: []string{"beta:archive"}},
	}})
}
func TestFilterPreviewsAndCommitsAcrossBothViews(t *testing.T) {
	for _, preset := range []int{0, 1} {
		t.Run(string(rune('0'+preset)), func(t *testing.T) {
			m := filterFixture(preset)
			m = filterText(m, "/")
			m = filterText(m, "ALPHA")
			view := stripStyles(m.View())
			if !strings.Contains(view, "alpha:photos") || strings.Contains(view, "beta:archive") || !strings.Contains(view, "1/2") {
				t.Fatalf("preview incorrect:\n%s", view)
			}
			m = selectionEnter(m)
			m, _ = helpKey(m, "down")
			m = selectionEnter(m)
			if !strings.Contains(stripStyles(m.View()), "alpha:photos") {
				t.Fatal("matching subject not reachable")
			}
			m, _ = helpKey(m, "esc")
			m, _ = helpKey(m, "esc")
			if !strings.Contains(stripStyles(m.View()), "beta:archive") {
				t.Fatal("Esc must clear committed filter")
			}
		})
	}
}

func TestFilterCancellationRestoresSelectionAndExclusionNeverReselects(t *testing.T) {
	m := filterFixture(0)
	m, _ = helpKey(m, "down")
	m = filterText(m, "/")
	m = filterText(m, "beta")
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("excluded subject remains selected")
	}
	m = filterKey(m, tea.KeyCtrlU)
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("widening preview reselected subject")
	}
	m = filterKey(m, tea.KeyEsc)
	if !strings.Contains(selectionFooter(m), "pid 11") {
		t.Fatalf("cancel did not restore initial selection: %s", selectionFooter(m))
	}
	m = filterText(m, "/")
	m = filterText(m, "beta")
	m = selectionEnter(m)
	m, _ = helpKey(m, "esc")
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("clearing applied filter reselected excluded subject")
	}
}
func TestFilterEditingUnicodePasteAndMonitorHotkeys(t *testing.T) {
	m := filterFixture(0)
	m = filterText(m, "/")
	m = filterText(m, "qhjkpP1+-")
	if !strings.Contains(selectionFooter(m), "qhjkpP1+-") {
		t.Fatal("monitor keys were not inserted literally")
	}
	m = filterKey(m, tea.KeyCtrlU)
	m = filterText(m, "a👨‍👩‍👧e\u0301")
	m = filterKey(m, tea.KeyBackspace)
	m = filterKey(m, tea.KeyBackspace)
	if !strings.Contains(selectionFooter(m), "/ a") || strings.Contains(selectionFooter(m), "👨") {
		t.Fatalf("grapheme deletion failed: %s", selectionFooter(m))
	}
	m = filterKey(m, tea.KeyHome)
	m = filterText(m, "x")
	m = filterKey(m, tea.KeyRight)
	m = filterKey(m, tea.KeyBackspace)
	m = filterKey(m, tea.KeyEnd)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q\nh\tj\rk"), Paste: true})
	m = next.(Model)
	if !strings.Contains(selectionFooter(m), "xq h j k") {
		t.Fatalf("paste not sanitized as text: %s", selectionFooter(m))
	}
	m = filterKey(m, tea.KeyHome)
	m = filterKey(m, tea.KeyDelete)
	if strings.Contains(selectionFooter(m), "xq") {
		t.Fatal("delete did not remove first grapheme")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Ctrl+C must quit while editing")
	}
}

func TestFilterDetailsStayLiveAndUpdatesRecomputeMatches(t *testing.T) {
	m := filterFixture(1)
	m = filterText(m, "/")
	m = filterText(m, "alpha")
	m = selectionEnter(m)
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: time.Now(), Processes: []model.Process{
		{PID: 11, Kind: model.KindCopy, StartedAt: time.Unix(100, 0), Paths: []string{"changed:photos"}},
		{PID: 22, Kind: model.KindSync, StartedAt: time.Unix(100, 0), Paths: []string{"alpha:new"}},
	}})
	view := stripStyles(m.View())
	if !strings.Contains(view, "changed:photos") || !strings.Contains(view, "Subject details") {
		t.Fatal("excluded detail stopped updating")
	}
	m = filterText(m, "/")
	if !strings.Contains(stripStyles(m.View()), "Subject details") {
		t.Fatal("slash changed detail mode")
	}
	m, _ = helpKey(m, "esc")
	view = stripStyles(m.View())
	if strings.Contains(view, "changed:photos") || !strings.Contains(view, "alpha:new") || strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatalf("monitor did not recompute matches/clear selection:\n%s", view)
	}
	m = filterText(m, "/")
	m = filterKey(m, tea.KeyCtrlU)
	m = filterText(m, "absent")
	m = selectionEnter(m)
	view = stripStyles(m.View())
	if !strings.Contains(view, "no matching subjects") || strings.Contains(view, "idle") || strings.Contains(view, "no rclone process running") {
		t.Fatalf("filter misreports idle:\n%s", view)
	}
}
func TestFilterFooterKeepsCursorAndIndicatorAtExtremeSizes(t *testing.T) {
	m := filterFixture(1)
	m = filterText(m, "/")
	m = filterText(m, strings.Repeat("x", 100)+"END")
	for _, width := range []int{1, 2, 3, 8, 25} {
		sized := helpSize(m, width, 1)
		view := stripStyles(sized.View())
		if strings.Contains(view, "\n") {
			t.Fatal("one row terminal must prioritize editor")
		}
		if width >= 8 && !strings.Contains(view, "END") {
			t.Fatalf("cursor tail hidden at %d: %q", width, view)
		}
		sized = selectionEnter(sized)
		view = stripStyles(sized.View())
		if view == "" || strings.Contains(view, "help") {
			t.Fatalf("filter indicator lost at %d: %q", width, view)
		}
	}
	m = helpSize(m, 10, 1)
	m = filterKey(m, tea.KeyHome)
	if strings.Contains(stripStyles(m.View()), "END") {
		t.Fatal("horizontal scroll did not follow Home")
	}
}
func TestFilterHelpExplainsEditorAndEscape(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 160, 100)
	m, _ = helpKey(m, "?")
	view := stripStyles(m.View())
	for _, want := range []string{"Ctrl+U", "Backspace", "Left / Right", "literal text", "confirm", "cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("help missing %q", want)
		}
	}
}

func TestFilterCancelFollowsOriginalSubjectThroughOwnerDiscoveryAndExit(t *testing.T) {
	start := time.Unix(100, 0)
	m := helpSize(New(nil, Options{}, nil), 120, 40)
	p := model.Process{PID: 11, Kind: model.KindCopy, StartedAt: start, Unit: "backup.service", UnitScope: "user"}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: time.Now(), Processes: []model.Process{p}})
	m, _ = helpKey(m, "down")
	m = filterText(m, "/")
	m = filterText(m, "absent")
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: time.Now(), Units: []model.Unit{{Name: "backup.service", Scope: "user", MainPID: 11}}})
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: time.Now(), Processes: []model.Process{}})
	m = filterKey(m, tea.KeyEsc)
	if !strings.Contains(selectionFooter(m), "backup.service (user)") {
		t.Fatalf("cancel lost stable identity: %s", selectionFooter(m))
	}
	m = filterText(m, "/")
	m = filterText(m, "absent")
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: time.Now(), Units: []model.Unit{}})
	m = filterKey(m, tea.KeyEsc)
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("cancel restored vanished subject")
	}
}

func TestFilterPreservesHostContextAndCountsHiddenSubjects(t *testing.T) {
	for _, preset := range []int{0, 1} {
		m := filterFixture(preset)
		m = selectionResult(m, model.Snapshot{Source: model.SourceLocalFS, At: time.Now(), Mounts: []model.Mount{{Remote: "orphan:host", Mountpoint: "/unserved"}}, Caches: []model.CacheDir{{Kind: "vfs", Bytes: 1024, Files: 5}}})
		m = selectionResult(m, model.Snapshot{Source: model.SourceBisync, At: time.Now(), SyncPairs: []model.SyncPair{{Left: model.SyncSide{Path: "host-left"}, Right: model.SyncSide{Path: "host-right"}}}})
		next, _ := m.Update(resultMsg(collect.Result{Source: model.SourceLog, Err: errors.New("host-source-unreadable")}))
		m = next.(Model)
		m = filterText(m, "/")
		m = filterText(m, "absent")
		m = selectionEnter(m)
		view := stripStyles(m.View())
		for _, want := range []string{"orphan:host", "host-left", "host-right", "vfs", "localfs", "bisync", "host-source-unreadable", "0/2"} {
			if !strings.Contains(view, want) {
				t.Fatalf("preset %d lost host context %q:\n%s", preset, want, view)
			}
		}
		m, _ = helpKey(m, "down")
		m = selectionEnter(m)
		if strings.Contains(stripStyles(m.View()), "Subject details") {
			t.Fatal("no matches must not open a detail")
		}
	}
	m := filterFixture(1)
	m = filterText(m, "/")
	m = filterText(m, "alpha")
	m = selectionEnter(m)
	for _, key := range []string{"1", "2", "3", "4"} {
		m, _ = helpKey(m, key)
	}
	if !strings.Contains(stripStyles(m.View()), "1/2") {
		t.Fatal("hidden panels changed subject count")
	}
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	if !strings.Contains(stripStyles(m.View()), "alpha:photos") {
		t.Fatal("hidden filtered subject unreachable")
	}
}
func TestFilterContinuesSamplingExcludedProcesses(t *testing.T) {
	m := filterFixture(0)
	m = filterText(m, "/")
	m = filterText(m, "absent")
	m = selectionEnter(m)
	start := time.Unix(100, 0)
	for i := 0; i < 4; i++ {
		next, cmd := m.Update(resultMsg(collect.Result{Snapshot: model.Snapshot{Source: model.SourceProc, At: time.Now(), Processes: []model.Process{{PID: 11, Kind: model.KindCopy, StartedAt: start, IOAvailable: true, ReadRate: 1024, Paths: []string{"sampled-while-excluded"}}}}}))
		m = next.(Model)
		if cmd == nil {
			t.Fatal("filter stopped collector rearming")
		}
	}
	m, _ = helpKey(m, "esc")
	view := stripStyles(m.View())
	if !strings.Contains(view, "sampled-while-excluded") || brailleWeight(view) == 0 {
		t.Fatal("excluded process samples were discarded")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("second Esc must retain normal quit behavior")
	}
}
func TestFilterDeletionKeepsCursorOnMergedGraphemeBoundary(t *testing.T) {
	m := filterFixture(0)
	m = filterText(m, "/")
	m = filterText(m, "🇦X🇧")
	m = filterKey(m, tea.KeyLeft)
	m = filterKey(m, tea.KeyBackspace)
	m = filterText(m, "Z")
	if !strings.Contains(selectionFooter(m), "🇦🇧Z") {
		t.Fatalf("deletion left cursor inside a merged grapheme: %q", selectionFooter(m))
	}
}
func TestEmptyFilterPreviewShowsAllSubjectCount(t *testing.T) {
	m := filterFixture(0)
	m = filterText(m, "/")
	if !strings.Contains(stripStyles(m.View()), "2/2") {
		t.Fatal("empty preview must still explain match count")
	}
}
