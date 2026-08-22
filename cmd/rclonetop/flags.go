package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// options holds the command line configuration.
//
// The short forms deliberately match btop's, so that muscle memory carries
// over: -u for the update rate, -p for the preset, -t to force TTY mode.
type options struct {
	configPath      string
	themeName       string
	themeBackground bool
	updateMS        int
	preset          int
	base10          bool
	vimKeys         bool
	tty             bool
	lowColor        bool
	debug           bool
	noAltScreen     bool
	showHelp        bool
	showVersion     bool
}

func parseFlags(args []string) (options, error) {
	var o options

	fs := flag.NewFlagSet("rclonetop", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // usage is printed by printUsage, not by flag

	str := func(target *string, def string, names ...string) {
		for _, n := range names {
			fs.StringVar(target, n, def, "")
		}
	}
	num := func(target *int, def int, names ...string) {
		for _, n := range names {
			fs.IntVar(target, n, def, "")
		}
	}
	boolean := func(target *bool, def bool, names ...string) {
		for _, n := range names {
			fs.BoolVar(target, n, def, "")
		}
	}

	str(&o.configPath, "", "c", "config")
	str(&o.themeName, "", "theme")
	num(&o.updateMS, 2000, "u", "update")
	num(&o.preset, 0, "p", "preset")
	boolean(&o.themeBackground, true, "theme-background")
	boolean(&o.base10, false, "base-10")
	boolean(&o.vimKeys, false, "vim-keys")
	boolean(&o.tty, false, "t", "tty")
	boolean(&o.lowColor, false, "l", "low-color")
	boolean(&o.debug, false, "d", "debug")
	boolean(&o.noAltScreen, false, "no-alt-screen")
	boolean(&o.showHelp, false, "h", "help")
	boolean(&o.showVersion, false, "V", "version")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			o.showHelp = true
			return o, nil
		}
		return o, err
	}
	if o.updateMS < 100 {
		return o, fmt.Errorf("--update must be at least 100 ms, got %d", o.updateMS)
	}
	return o, nil
}

func printUsage(w io.Writer) {
	usage := strings.TrimLeft(`
rclonetop - a terminal monitor for rclone, in the style of btop++

Usage:
  rclonetop [options]

Options:
  -c, --config <file>     path to the configuration file
      --theme <name>      colour theme; btop themes are found automatically
      --theme-background  use the theme's background colour (default true)
  -u, --update <ms>       refresh interval in milliseconds (default 2000)
  -p, --preset <0-9>      initial layout preset (default 0, the dense view)
      --base-10           size units in KB=1000 instead of KiB=1024
      --vim-keys          navigate with h j k l g G
  -t, --tty               force TTY mode: 8 colours and block graphs
  -l, --low-color         limit output to 256 colours
      --no-alt-screen     draw in place instead of on the alternate screen
  -d, --debug             log verbosely
  -h, --help              show this message
  -V, --version           show the version

Keys:
  q, Esc                  quit
  +, -                    refresh faster or slower

rclonetop reads only. It never changes rclone's configuration, starts or stops
transfers, or writes to any remote.
`, "\n")
	fmt.Fprint(w, usage)
}
