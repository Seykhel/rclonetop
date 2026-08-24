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
	"github.com/Seykhel/rclonetop/internal/config"
	"github.com/Seykhel/rclonetop/internal/theme"
	"github.com/Seykhel/rclonetop/internal/ui"
	"github.com/Seykhel/rclonetop/internal/ui/graph"
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
	if flags.defaultConfig {
		// Printed rather than written. btop rewrites its own configuration on
		// exit; rclonetop cannot do that and stay read-only, so where the file
		// lands -- and whether it lands at all -- is left to the user's shell.
		fmt.Print(config.DefaultFile(ui.Version))
		return nil
	}

	// The file is laid underneath the command line rather than over it, so that
	// a flag typed now always beats a setting saved months ago.
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		// -d is the exception, and deliberately so: it is what users are asked
		// to paste into a bug report, and a host whose configuration file will
		// not parse is exactly the sort that gets reported. Making the
		// diagnostic depend on the thing being diagnosed would take it away
		// when it is most needed. Everywhere else a file the user wrote wrong
		// is worth stopping for, since they cannot see the message once the
		// alternate screen is up.
		if !flags.debug {
			return err
		}
		fmt.Fprintln(os.Stderr, "rclonetop:", err, "- using the built-in defaults")
		cfg = config.Defaults()
	}
	flags = applyConfig(flags, cfg)

	th, err := theme.Load(flags.themeName)
	if err != nil {
		// A missing theme is not worth refusing to start over; fall back and
		// say so once, on stderr, where it will not corrupt the display.
		fmt.Fprintln(os.Stderr, "rclonetop:", err, "- using the built-in theme")
		th = theme.Default()
	}
	// Clamping the colour profile makes lipgloss quantise every colour down
	// to the requested palette, so the same gradients keep working on a
	// terminal that cannot render them at full depth. Which glyphs to draw was
	// already settled by applyConfig, where it could still be told who asked.
	symbol := graph.Symbol(flags.graphSymbol)

	switch {
	case flags.tty:
		lipgloss.SetColorProfile(termenv.ANSI)
		// A real console has neither the colours nor the glyphs, so forcing TTY
		// mode replaces the theme as well -- unless the theme was named on the
		// command line and force_tty only came from the file, which would be
		// the file overruling a decision made just now.
		if flags.explicit["tty"] || !flags.explicit["theme"] {
			th = theme.TTY()
		}
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

	// Three collectors know something a fourth needs, and none of them can find
	// it out alone.
	//
	// The systemd collector cannot tell which units drive rclone when they run
	// it from a wrapper script; a live rclone's cgroup names its unit exactly.
	// The log collector has no way to guess which files to follow; a live
	// rclone's command line names them. And the bisync collector cannot undo
	// the mangling in its listing filenames; the log writes the paths out in
	// full. In each case the process collector is already looking at the right
	// thing at the right moment, so it passes it on.
	systemd := collect.NewSystemd()
	bisync := collect.NewBisync()
	logs := collect.NewLogs()
	procs := collect.NewProcs()
	procs.OnProcesses(systemd.NoteProcesses)
	procs.OnProcesses(logs.NoteProcesses)
	logs.OnPaths(bisync.NotePaths)
	// And the other way for a job that is not running: the unit names the log
	// file even between runs, which on a timer is nearly all of the time.
	systemd.OnLogFiles(logs.NoteUnitLogs)

	// The order is the order the facts travel in, and it matters for -d, which
	// runs them in turn rather than in parallel: processes name the units and
	// the logs of what is running now, units name the logs of what is not, and
	// the logs name the paths the bisync listings cannot spell. Collected the
	// other way round, -d would show each collector missing what the one after
	// it was about to tell it.
	collectors := []collect.Collector{
		procs,
		systemd,
		logs,
		bisync,
		collect.NewLocalFS(),
	}

	if flags.debug {
		return dump(ctx, os.Stdout, collectors, flags.base10)
	}

	results := collect.Run(ctx, collectors)

	m := ui.New(results, ui.Options{
		Theme:       th,
		UpdateMS:    flags.updateMS,
		Base10:      flags.base10,
		GraphSymbol: symbol,
		ClockLayout: flags.clockLayout,
		Host:        host,
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
