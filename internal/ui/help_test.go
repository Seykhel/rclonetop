package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
)

func helpKey(m Model, key string) (Model, tea.Cmd) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		msg = tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		msg = tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		msg = tea.KeyMsg{Type: tea.KeyPgDown}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func helpSize(m Model, width, height int) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return next.(Model)
}

func TestHelpOpensAndReturnsToTheMonitor(t *testing.T) {
	for _, opener := range []string{"?", "h"} {
		for _, closer := range []string{"?", "h", "esc"} {
			t.Run(opener+"/"+closer, func(t *testing.T) {
				m := helpSize(New(nil, Options{}, nil), 100, 35)
				before := m.View()
				m, _ = helpKey(m, opener)
				if !strings.Contains(stripStyles(m.View()), "Keyboard help") {
					t.Fatal("opening help should display the keyboard reference")
				}
				m, cmd := helpKey(m, closer)
				if cmd != nil || m.View() != before {
					t.Fatal("closing help should restore the monitor without quitting")
				}
			})
		}
	}
}

func TestHelpConsumesMonitorCommands(t *testing.T) {
	for _, preset := range []int{0, 1} {
		m := helpSize(New(nil, Options{Preset: preset, Presets: [10]string{"", "", "transfers:0:1"}}, nil), 120, 40)
		before := m.View()
		m, _ = helpKey(m, "?")
		for _, key := range []string{"p", "P", "1", "2", "3", "4", "+", "=", "-", "_"} {
			m, _ = helpKey(m, key)
			if !strings.Contains(stripStyles(m.View()), "Keyboard help") {
				t.Fatalf("%q dismissed help", key)
			}
		}
		m, _ = helpKey(m, "esc")
		if m.View() != before {
			t.Fatalf("monitor commands changed the view under help (preset %d)", preset)
		}
	}
}

func TestQuitKeysStillExitWithHelpOpen(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c", "esc"} {
		m := New(nil, Options{}, nil)
		if key != "esc" {
			m, _ = helpKey(m, "?")
		}
		m, cmd := helpKey(m, key)
		if cmd == nil {
			t.Fatalf("%q should return a quit command", key)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok || m.View() != "" {
			t.Fatalf("%q should quit", key)
		}
	}
}

func TestHelpListsEveryCommandInACenteredFrame(t *testing.T) {
	for _, preset := range []int{0, 1} {
		for _, symbol := range []graph.Symbol{graph.Braille, graph.TTY} {
			m := helpSize(New(nil, Options{Preset: preset, GraphSymbol: symbol, Host: "visible-host"}, nil), 120, 40)
			m, _ = helpKey(m, "?")
			view := stripStyles(m.View())
			if strings.Count(view, "Keyboard help") != 1 {
				t.Fatalf("help should have one title, not a repeated heading:\n%s", view)
			}
			for _, text := range []string{
				"Keyboard help", "Framed panels", "visible-host",
			} {
				if !strings.Contains(view, text) {
					t.Fatalf("missing %q in help:\n%s", text, view)
				}
			}
			// Match keys together with their action, rather than finding a
			// single digit or letter somewhere in the reference/background.
			words := strings.Join(strings.Fields(view), " ")
			for _, command := range []string{
				"? / h Open or close help",
				"Esc Close help/details; clear filter; otherwise quit",
				"Up Select previous subject; scroll help/details up",
				"Down Select next subject; scroll help/details down",
				"PgUp Scroll help/details up by one page",
				"PgDn Scroll help/details down by one page",
				"p Alternate dense and remembered framed view",
				"P Cycle configured framed presets (enter framed from dense)",
				"1 Toggle transfers", "2 Toggle bandwidth", "3 Toggle files", "4 Toggle status",
				"+ / = Refresh faster (halve interval, minimum 100ms)",
				"- / _ Refresh slower (double interval, maximum 30000ms)",
				"q / Ctrl+C Quit from monitor, help or details",
			} {
				if !strings.Contains(words, command) {
					t.Fatalf("missing command %q in help:\n%s", command, view)
				}
			}
			lines := strings.Split(view, "\n")
			var left, right, top, bottom int
			for y, line := range lines {
				if x := strings.Index(line, "╭"); x >= 0 && strings.Contains(line, "┐Keyboard help┌") {
					top, left, right = y, lipgloss.Width(line[:x]), lipgloss.Width(strings.Split(line[x:], "╮")[0])+1
				}
				if y > top && left > 0 && len([]rune(line)) > left && []rune(line)[left] == '╰' {
					bottom = y
					break
				}
			}
			if symbol == graph.TTY {
				if !strings.Contains(view, "[Keyboard help]") || strings.ContainsAny(view, "╭╮╰╯│") {
					t.Fatal("console help should use an ASCII frame")
				}
			} else if left < 1 || absHelp(left-(120-left-right)) > 1 || absHelp(top-(40-bottom-1)) > 1 {
				t.Fatalf("help frame should be centered: left=%d width=%d top=%d bottom=%d", left, right, top, bottom)
			}
		}
	}
}

func absHelp(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func TestHelpScrollsByLineAndPageAndRestartsOnReopening(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 70, 6)
	m, _ = helpKey(m, "?")
	initial := m.View()
	if !strings.Contains(stripStyles(initial), "Keyboard help") || !strings.Contains(stripStyles(initial), "Esc / ? / h close") {
		t.Fatal("opening help must show its title and close hint")
	}
	m, _ = helpKey(m, "up")
	m, _ = helpKey(m, "pgup")
	if m.View() != initial {
		t.Fatal("scrolling up at the beginning should stay at the beginning")
	}
	m, _ = helpKey(m, "down")
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); first != "Help" {
		t.Fatalf("Down should advance one line, got %q", first)
	}
	m, _ = helpKey(m, "up")
	if m.View() != initial {
		t.Fatal("Up should return to the initial line")
	}
	m, _ = helpKey(m, "pgdown")
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); !strings.HasPrefix(first, "Down ") {
		t.Fatalf("Page Down should advance the five visible content rows, got %q", first)
	}
	for i := 0; i < 30; i++ {
		m, _ = helpKey(m, "pgdown")
	}
	end := m.View()
	if !strings.Contains(stripStyles(end), "q / Ctrl+C") || !strings.Contains(stripStyles(end), "Esc / ? / h close") {
		t.Fatal("last command and close hint should remain reachable")
	}
	m, _ = helpKey(m, "down")
	m, _ = helpKey(m, "pgdown")
	if m.View() != end {
		t.Fatal("scrolling at the end should stay at the end")
	}
	m, _ = helpKey(m, "esc")
	m, _ = helpKey(m, "h")
	if m.View() != initial {
		t.Fatal("reopening should start at the beginning")
	}
}

func TestHelpResizeKeepsTheCommandBeingRead(t *testing.T) {
	m := helpSize(New(nil, Options{}, nil), 80, 5)
	m, _ = helpKey(m, "?")
	for i := 0; i < 40 && !strings.HasPrefix(strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]), "P "); i++ {
		m, _ = helpKey(m, "down")
	}
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); !strings.HasPrefix(first, "P ") {
		t.Fatalf("expected to be reading P, got %q", first)
	}
	m = helpSize(m, 35, 5)
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); !strings.HasPrefix(first, "P ") {
		t.Fatalf("resize should keep P visible instead of its old row number: %q", first)
	}
	// Scroll to a continuation of the command, then resize wider. It still
	// belongs to P, even though that wrapped line will no longer exist.
	m, _ = helpKey(m, "down")
	m = helpSize(m, 80, 5)
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); !strings.HasPrefix(first, "P ") {
		t.Fatalf("unwrapping should keep the current command visible: %q", first)
	}
	m = helpSize(m, 120, 40)
	if !strings.Contains(stripStyles(m.View()), "┐Keyboard help┌") {
		t.Fatal("growing should restore the complete framed reference")
	}
	m = helpSize(m, 80, 5)
	if first := strings.TrimSpace(strings.Split(stripStyles(m.View()), "\n")[0]); !strings.HasPrefix(first, "P ") {
		t.Fatalf("shrinking the overlay should restore the command being read: %q", first)
	}
}

func TestMonitorFooterKeepsWholeHintsInPriorityOrder(t *testing.T) {
	for _, preset := range []int{0, 1} {
		for _, tc := range []struct {
			width int
			want  string
		}{
			{5, ""},
			{6, "? help"},
			{13, "? help"},
			{14, "? help  q quit"},
			{21, "? help  q quit"},
			{22, "? help  q quit  p view"},
		} {
			m := helpSize(New(nil, Options{Preset: preset}, nil), tc.width, 40)
			lines := strings.Split(stripStyles(m.View()), "\n")
			got := strings.TrimSpace(lines[len(lines)-1])
			if got != tc.want {
				t.Errorf("preset %d width %d: footer %q, want %q", preset, tc.width, got, tc.want)
			}
		}
		m := helpSize(New(nil, Options{Preset: preset}, nil), 180, 40)
		lines := strings.Split(stripStyles(m.View()), "\n")
		footer := lines[len(lines)-1]
		for _, hint := range []string{"? help", "q quit", "p view", "P next", "2000ms", "sources "} {
			if !strings.Contains(footer, hint) {
				t.Errorf("wide footer should contain %q: %s", hint, footer)
			}
		}
		if preset == 1 && !strings.Contains(footer, "preset 1  P next") {
			t.Errorf("framed footer should include current preset and next key: %s", footer)
		}
	}
}

func TestHelpKeepsCollectingAndAdvancingTheClock(t *testing.T) {
	start := time.Unix(1787000000, 0)
	m := helpSize(New(nil, Options{}, nil), 100, 35)
	m, _ = helpKey(m, "?")
	for i := 0; i < 4; i++ {
		next, cmd := m.Update(resultMsg(collect.Result{Snapshot: model.Snapshot{
			Source: model.SourceProc, At: start.Add(time.Duration(i) * time.Second),
			Processes: []model.Process{{
				Source: model.SourceProc, PID: 1234, Kind: model.KindCopy,
				StartedAt: start, IOAvailable: true, ReadRate: 1024,
				Paths: []string{"collected-during-help"},
			}},
		}}))
		m = next.(Model)
		if cmd == nil || !strings.Contains(stripStyles(m.View()), "Keyboard help") {
			t.Fatal("collector results should rearm collection while keeping help open")
		}
	}
	next, cmd := m.Update(tickMsg(start.Add(10 * time.Second)))
	m = next.(Model)
	if cmd == nil {
		t.Fatal("the clock should rearm while help is open")
	}
	m, _ = helpKey(m, "esc")
	view := stripStyles(m.View())
	for _, text := range []string{"collected-during-help", start.Add(10 * time.Second).Format("15:04:05"), "1.0 KiB/s"} {
		if !strings.Contains(view, text) {
			t.Fatalf("monitor should show updated %q after closing help:\n%s", text, view)
		}
	}
	if brailleWeight(view) == 0 {
		t.Fatal("process samples collected under help should produce a visible graph")
	}
}

func TestHelpFitsSmallTerminalsAndKeepsACloseHint(t *testing.T) {
	for _, symbol := range []graph.Symbol{graph.Braille, graph.TTY} {
		for _, size := range [][2]int{{1, 1}, {2, 2}, {3, 1}, {9, 3}, {20, 6}, {35, 8}, {70, 10}, {80, 20}, {120, 40}, {0, 0}} {
			m := helpSize(New(nil, Options{Preset: 1, GraphSymbol: symbol}, nil), size[0], size[1])
			m, _ = helpKey(m, "?")
			for _, key := range []string{"", "pgdown", "down", "pgup", "up"} {
				if key != "" {
					m, _ = helpKey(m, key)
				}
				lines := strings.Split(stripStyles(m.View()), "\n")
				width, height := size[0], size[1]
				if width == 0 {
					width = 80
				}
				if height == 0 {
					height = 24
				}
				if len(lines) > height {
					t.Fatalf("size %v key %q: %d rows exceeds height", size, key, len(lines))
				}
				for _, line := range lines {
					if lipgloss.Width(line) > width {
						t.Fatalf("size %v key %q: row too wide: %q", size, key, line)
					}
				}
				if width >= 3 && !strings.Contains(strings.ToLower(stripStyles(m.View())), "esc") {
					t.Fatalf("size %v key %q: close hint disappeared", size, key)
				}
				if width < 3 && strings.TrimSpace(lines[len(lines)-1]) != "?" {
					t.Fatalf("size %v: show a complete close key rather than a fragment: %q", size, lines[len(lines)-1])
				}
			}
			m, cmd := helpKey(m, "esc")
			if cmd != nil || m.View() == "" {
				t.Fatalf("size %v: Esc should still return to monitor", size)
			}
		}
	}
}
