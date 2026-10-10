package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
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

func TestSelectionSurvivesOwnerDiscoveryThenProcessExit(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 120, 60)
	p := model.Process{PID: 11, Kind: model.KindCopy, StartedAt: start, Unit: "backup.service", UnitScope: "user"}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{p}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: start, Units: []model.Unit{{Name: "backup.service", Scope: "user", ActiveState: "active", MainPID: 11}}})
	if !strings.Contains(stripStyles(m.View()), "backup.service (user)") {
		t.Fatal("owner discovery lost selected process")
	}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{}})
	if strings.Contains(stripStyles(m.View()), "Subject no longer available") {
		t.Fatal("service transition lost selection")
	}
	m, _ = helpKey(m, "esc")
	if !strings.Contains(selectionFooter(m), "backup.service (user)") {
		t.Fatal("monitor lost service identity")
	}
}

func TestDetailsWrapAllRetainedErrorsAndKeepTheirSources(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 33, 8)
	u := model.Unit{Name: "backup.service", Scope: "system", LogFile: "/tmp/backup.log", Errors: []model.LogLine{{At: start, Message: "journal original warning"}}}
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: start, Units: []model.Unit{u}})
	message := "log failure begins " + strings.Repeat("difficult-operation-", 12) + " END-OF-ERROR"
	m = selectionResult(m, model.Snapshot{Source: model.SourceLog, At: start, Jobs: []model.Job{{LogFile: u.LogFile, At: start, Errors: []model.LogLine{{At: start, Message: message}}, HaveStats: true, Stats: model.JobStats{Known: model.StatsBytes, Bytes: 0, Source: model.SourceLog}}}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	var read strings.Builder
	for i := 0; i < 100; i++ {
		read.WriteString(stripStyles(m.View()))
		m, _ = helpKey(m, "down")
	}
	got := strings.Join(strings.Fields(read.String()), " ")
	for _, want := range []string{"[systemd]", "journal original warning", "[log]", "END-OF-ERROR", "Transferred bytes: 0 B", "Total bytes: unknown"} {
		if !strings.Contains(got, want) {
			t.Fatalf("detail lost %q", want)
		}
	}
}

func TestDetailsFitExtremeDimensionsAndQuitGlobally(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {2, 2}, {3, 1}, {9, 3}, {20, 6}, {80, 24}, {120, 60}, {0, 0}} {
		m := helpSize(New(nil, Options{}, nil), size[0], size[1])
		m = selectionResult(m, model.Snapshot{Source: model.SourceProc, Processes: []model.Process{{PID: 11, Kind: model.KindCopy}}})
		m, _ = helpKey(m, "down")
		m = selectionEnter(m)
		for _, key := range []string{"", "pgdown", "up", "?", "esc"} {
			if key != "" {
				m, _ = helpKey(m, key)
			}
			lines := strings.Split(stripStyles(m.View()), "\n")
			if len(lines) > effectiveHeight(size[1]) {
				t.Fatalf("size %v exceeds height", size)
			}
			for _, line := range lines {
				if lipgloss.Width(line) > effectiveWidth(size[0]) {
					t.Fatalf("size %v exceeds width: %q", size, line)
				}
			}
		}
		m, cmd := helpKey(m, "q")
		if cmd == nil || m.View() != "" {
			t.Fatal("q must quit details")
		}
	}
}

func TestDetailsIncludeAvailableDaemonFactsAndFileMeasurements(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 100, 12)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{{PID: 11, Kind: model.KindRCD, RCAddr: "localhost:5572"}}})
	m = selectionResult(m, model.Snapshot{Source: model.SourceRC, At: start, RCStats: []model.RCStats{{Addr: "localhost:5572", At: start, Daemon: model.RCDaemon{Version: "v1.99", Memory: model.RCMemory{HeapAllocSet: true, HeapAlloc: 1024}}, Jobs: []model.RCJob{{ID: 42, Error: "job-failure", SuccessKnown: true, Finished: true}}, Transferring: []model.Transfer{{Name: "full-name", Percentage: 25, BytesKnown: true, Bytes: 1024, Size: 4096, Speed: 128}}}}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	var read strings.Builder
	for i := 0; i < 120; i++ {
		read.WriteString(m.View())
		m, _ = helpKey(m, "down")
	}
	got := strings.Join(strings.Fields(stripStyles(read.String())), " ")
	for _, want := range []string{"v1.99", "Heap allocated: 1.0 KiB", "System memory: unknown", "RC job 42", "job-failure", "full-name", "25%", "1.0 KiB", "4.0 KiB"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestDetailKeepsReadingPositionAcrossResizeAndLiveEvidence(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 45, 6)
	u := model.Unit{Name: "backup.service", Scope: "user", LogFile: "/tmp/a.log", ActiveState: "inactive", Result: "exit-code"}
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: start, Units: []model.Unit{u}})
	m = selectionResult(m, model.Snapshot{Source: model.SourceLog, At: start, Jobs: []model.Job{{LogFile: u.LogFile, Errors: []model.LogLine{{At: start, Message: "ERROR-READING-POSITION"}}}}})
	for _, source := range []model.Source{model.SourceProc, model.SourceRC, model.SourceBisync, model.SourceLocalFS} {
		m = selectionResult(m, model.Snapshot{Source: source, At: start})
	}
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	first := func() string { return strings.Split(stripStyles(m.View()), "\n")[0] }
	for i := 0; i < 100 && !strings.Contains(first(), "[log]"); i++ {
		m, _ = helpKey(m, "down")
	}
	if !strings.Contains(first(), "[log]") {
		t.Fatal("error must be reachable")
	}
	m = helpSize(m, 70, 6)
	if !strings.Contains(first(), "[log]") {
		t.Fatal("resize lost reading entry")
	}
	u.ActiveState = "activating"
	u.Result = "success"
	u.InactiveExit = start.Add(time.Hour)
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, At: start.Add(time.Hour), Units: []model.Unit{u}})
	if !strings.Contains(first(), "[log]") {
		t.Fatal("new run moved reading position")
	}
	m, _ = helpKey(m, "esc")
	m = selectionEnter(m)
	var read strings.Builder
	for i := 0; i < 70; i++ {
		read.WriteString(stripStyles(m.View()))
		m, _ = helpKey(m, "down")
	}
	if !strings.Contains(read.String(), "Service: running") {
		t.Fatal("details did not update to new run")
	}
}

func TestDetailAndNestedHelpKeepCollectingAndSampling(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 90, 10)
	p := model.Process{PID: 11, Kind: model.KindCopy, StartedAt: start, IOAvailable: true, ReadRate: 1024}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{p}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	m, _ = helpKey(m, "?")
	for i := 1; i < 4; i++ {
		p.Paths = []string{"live-operand"}
		next, cmd := m.Update(resultMsg(collect.Result{Snapshot: model.Snapshot{Source: model.SourceProc, At: start.Add(time.Duration(i) * time.Second), Processes: []model.Process{p}}}))
		m = next.(Model)
		if cmd == nil {
			t.Fatal("collector not rearmed")
		}
	}
	next, cmd := m.Update(tickMsg(start.Add(time.Hour)))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("clock not rearmed")
	}
	m, _ = helpKey(m, "esc")
	m, _ = helpKey(m, "esc")
	if !strings.Contains(stripStyles(m.View()), "live-operand") || brailleWeight(stripStyles(m.View())) == 0 {
		t.Fatal("live samples lost under nested modal views")
	}
}

func TestEmptySelectionAndPIDReuseDoNotSelectAnotherSubject(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 80, 20)
	m, _ = helpKey(m, "up")
	m = selectionEnter(m)
	if strings.Contains(stripStyles(m.View()), "Subject details") {
		t.Fatal("empty list must not open detail")
	}
	start := time.Unix(1787000000, 0)
	p := model.Process{PID: 11, StartedAt: start, Kind: model.KindCopy}
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, Processes: []model.Process{p}})
	m, _ = helpKey(m, "up")
	m = selectionEnter(m)
	p.StartedAt = start.Add(time.Hour)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, Processes: []model.Process{p}})
	if !strings.Contains(stripStyles(m.View()), "Subject no longer available") {
		t.Fatal("PID reuse must not preserve selection")
	}
	m, _ = helpKey(m, "esc")
	if strings.Contains(selectionFooter(m), "Enter details") {
		t.Fatal("vanished selection must clear rather than move")
	}
}

func TestVimKeysRemainOptInAndArrowsAlwaysWork(t *testing.T) {
	for _, vim := range []bool{false, true} {
		m := helpSize(New(nil, Options{VimKeys: vim}, nil), 100, 20)
		m = selectionResult(m, model.Snapshot{Source: model.SourceProc, Processes: []model.Process{{PID: 11, Kind: model.KindCopy}}})
		m, _ = helpKey(m, "j")
		if strings.Contains(selectionFooter(m), "Enter details") != vim {
			t.Fatal("Vim aliases must require option")
		}
		m, _ = helpKey(m, "down")
		if !strings.Contains(selectionFooter(m), "Enter details") {
			t.Fatal("arrows must always work")
		}
	}
}

func TestDetailsExplainFailedSourcesAndUnreadableLogs(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 100, 12)
	m = selectionResult(m, model.Snapshot{Source: model.SourceProc, At: start, Processes: []model.Process{{PID: 11, Kind: model.KindCopy}}})
	m = selectionResult(m, model.Snapshot{Source: model.SourceLog, At: start, Jobs: []model.Job{{PID: 11, ReadError: "permission denied"}}})
	m, _ = helpKey(m, "down")
	m = selectionEnter(m)
	next, cmd := m.Update(resultMsg(collect.Result{Source: model.SourceProc, Err: fmt.Errorf("proc read failed")}))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("failed collector must rearm")
	}
	var read strings.Builder
	for i := 0; i < 80; i++ {
		read.WriteString(stripStyles(m.View()))
		m, _ = helpKey(m, "down")
	}
	got := strings.Join(strings.Fields(read.String()), " ")
	for _, want := range []string{"permission denied", "source failed, retained data may be stale", "proc read failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing source failure %q", want)
		}
	}
}

func TestSelectedFooterPrioritizesIdentityAndDetailsOverMonitorHints(t *testing.T) {
	for _, width := range []int{20, 30, 45} {
		m := helpSize(New(nil, Options{}, nil), width, 20)
		m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, Units: []model.Unit{{Name: "extraordinarily-long-backup.service", Scope: "system"}}})
		m, _ = helpKey(m, "down")
		footer := selectionFooter(m)
		if !strings.Contains(footer, "Enter details") || strings.HasPrefix(footer, "? help") {
			t.Fatalf("width %d selection footer lost priority: %q", width, footer)
		}
	}
}

func TestConsoleDetailsUseASCIIFrameAndSelectionMarker(t *testing.T) {
	m := helpSize(New(nil, Options{GraphSymbol: graph.TTY}, nil), 120, 70)
	m = selectionResult(m, model.Snapshot{Source: model.SourceSystemd, Units: []model.Unit{{Name: "console-backup.service", Scope: "user"}}})
	m, _ = helpKey(m, "down")
	if !strings.Contains(stripStyles(m.View()), "> UNIT") {
		t.Fatal("console selection must not depend on colour")
	}
	m = selectionEnter(m)
	view := stripStyles(m.View())
	if !strings.Contains(view, "[Subject details]") || strings.ContainsAny(view, "╭╮╰╯│") {
		t.Fatal("console detail must use ASCII geometry")
	}
}
