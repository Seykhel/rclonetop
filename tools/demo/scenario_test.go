package main

import (
	"strings"
	"testing"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

func TestScenarioThroughUIInput(t *testing.T) {
	ch := make(chan collect.Result, 1)
	var m tea.Model = ui.New(ch, demoOptions(), nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	// Init batches the channel waiter first and the clock second. Execute only
	// the waiter: the scenario test must never wait for a real clock tick.
	batch := m.Init()().(tea.BatchMsg)
	next := batch[0]
	for step := 0; step <= lastStep; step++ {
		for _, result := range scenario(step) {
			ch <- result
			m, next = m.Update(next())
		}
		if step == 3 {
			if got := m.View(); !strings.Contains(got, "report.pdf") || !strings.Contains(got, "25%") {
				t.Fatalf("missing transfer progress:\n%s", got)
			}
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
			if got := m.View(); !strings.Contains(got, "bandwidth") {
				t.Fatalf("p did not enter framed view:\n%s", got)
			}
		}
	}
	if got := m.View(); !strings.Contains(got, "successful") || strings.Contains(got, "report.pdf") {
		t.Fatalf("missing completed outcome or retained in-flight file:\n%s", got)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if got := m.View(); strings.Contains(got, "bandwidth") || !strings.Contains(got, "successful") {
		t.Fatalf("dense view lost outcome:\n%s", got)
	}
}
