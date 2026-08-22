// Command rclonetop is a terminal monitor for rclone in the style of btop++.
//
// It shows what rclone is doing on this host: running mounts and transfers,
// throughput, synchronised files and space used. It does not require rclone to
// be started in any particular way, and it never writes anything.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "rclonetop:", err)
		os.Exit(1)
	}
}

func run() error {
	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		return err
	}
	if flags.showHelp {
		printUsage(os.Stdout)
		return nil
	}
	if flags.showVersion {
		fmt.Println("rclonetop", ui.Version)
		return nil
	}

	th, err := theme.Load(flags.themeName)
	if err != nil {
		// A missing theme is not worth refusing to start over; fall back and
		// say so once, on stderr, where it will not corrupt the display.
		fmt.Fprintln(os.Stderr, "rclonetop:", err, "- using the built-in theme")
		th = theme.Default()
	}
	// Clamping the colour profile makes lipgloss quantise every colour down
	// to the requested palette, so the same gradients keep working on a
	// terminal that cannot render them at full depth.
	switch {
	case flags.tty:
		th = theme.TTY()
		lipgloss.SetColorProfile(termenv.ANSI)
	case flags.lowColor:
		lipgloss.SetColorProfile(termenv.ANSI256)
	}
	th.SetOpaqueBackground(flags.themeBackground)

	// The context is cancelled either by a signal or by the user quitting, and
	// that is what stops every collector goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	host, _ := os.Hostname()

	collectors := []collect.Collector{
		collect.NewProcs(),
	}

	if flags.debug {
		return dump(ctx, os.Stdout, collectors, flags.base10)
	}

	results := collect.Run(ctx, collectors)

	m := ui.New(results, ui.Options{
		Theme:    th,
		UpdateMS: flags.updateMS,
		Base10:   flags.base10,
		Host:     host,
	}, cancel)

	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if !flags.noAltScreen {
		opts = append(opts, tea.WithAltScreen())
	}

	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		return err
	}
	return nil
}
