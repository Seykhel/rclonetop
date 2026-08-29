package config

import "strings"

// defaultFile is the commented configuration --default-config prints.
//
// It is written by hand rather than generated from the Config struct, because
// what makes it worth printing is the prose: the reason each key exists and
// what the values mean. A generated file would list the same keys and teach
// nothing, and this is the only documentation of the format that travels with
// the binary.
//
// Written by hand, though, it can drift from the defaults it claims to show.
// TestDefaultFileRoundTrips is what stops that: it parses this text and
// requires the result to equal Defaults() exactly.
//
// The version is substituted by Replace rather than by Sprintf because the text
// talks about strftime, and a "%X" in the prose would be eaten by a format verb
// on the way out.
const defaultFile = `#? Configuration file for rclonetop v{{version}}
#?
#? rclonetop never writes this file. Print a fresh copy with
#?
#?     rclonetop --default-config > ~/.config/rclonetop/rclonetop.conf
#?
#? and edit it there. Keys are btop's wherever the meaning is the same, so a
#? setting you have already tuned in btop.conf reads the same here. Any key this
#? version does not recognise is ignored rather than refused, so a file shared
#? between machines running different versions still works.
#?
#? A flag given on the command line overrides the value here.

#* Name of a btop++/bpytop/bashtop formatted ".theme" file, without the
#* extension. "default" and "tty" are built in and need no file. Themes are
#* looked for in rclonetop's own theme directories and then in btop's, so any
#* theme already installed for btop can be named here as is.
color_theme = "default"

#* Whether to paint the theme's own background. Set to False to let the
#* terminal's background -- and any transparency -- show through.
theme_background = True

#* Glyphs the throughput sparklines are drawn from:
#*   braille  two columns of samples per cell, the densest
#*   block    eighth-blocks, for fonts whose braille is missing or misaligned
#*   tty      plain ASCII, for a Linux console
#* Leave it empty to let rclonetop choose, which is braille normally and plain
#* ASCII when force_tty says the terminal is a console. Naming a symbol here is
#* a choice, and force_tty will not override it.
graph_symbol = ""

#* Redraw interval in milliseconds. This is how often the screen is repainted,
#* not how often each source is read: the collectors run on their own intervals,
#* from one second for throughput to thirty for the bisync listings.
update_ms = 2000

#* Report sizes in KB = 1000 bytes rather than KiB = 1024. rclone itself counts
#* in binary units, so leaving this False is what makes rclonetop's figures
#* comparable with rclone's own output.
base_10_sizes = False

#* Assume a Linux console: the eight-colour built-in theme, and ASCII graphs
#* unless graph_symbol says otherwise. Overrides color_theme when True.
force_tty = False

#* Whether the terminal can render 24-bit colour. Set to False to quantise every
#* colour down to the 256-colour palette; the gradients still work, with visible
#* banding. Ignored when force_tty is True, which is stricter still.
truecolor = True

#* Clock in the header, given as a Go reference layout -- the time written out
#* for Mon Jan 2 15:04:05 MST 2006. "15:04:05" is 24-hour with seconds,
#* "3:04PM" is 12-hour without. btop calls this clock_format and means a
#* strftime string; the name differs here because the value does, and "%X" is
#* refused with that explanation rather than printed literally.
clock_layout = "15:04:05"

#* Which framed-view panels to start with, out of "transfers bandwidth files
#* status", space separated. Leave empty to show every panel that exists --
#* which is also what a file written before a later version added a panel
#* keeps meaning, rather than freezing on today's four. A name this version
#* does not recognise is dropped rather than refused.
shown_boxes = ""
`

// DefaultFile returns the commented default configuration, stamped with the
// running version so a file found on disk years later says what wrote it.
func DefaultFile(version string) string {
	return strings.Replace(defaultFile, "{{version}}", version, 1)
}
