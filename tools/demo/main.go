// Command demo runs the real monitor with isolated synthetic observations.
// It is development tooling only; release packaging builds cmd/rclonetop.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func main() {
	// The recording has a fixed true-color profile, independent of host detection.
	lipgloss.SetColorProfile(termenv.TrueColor)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan collect.Result)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(results)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for step := 0; step <= lastStep; step++ {
			for _, result := range scenario(step) {
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
			if step < lastStep {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	_, err := tea.NewProgram(ui.New(results, demoOptions(), cancel), tea.WithAltScreen()).Run()
	cancel()
	<-done
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
