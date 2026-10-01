package theme

import "sync"

const (
	defaultName = "default"
	ttyName     = "tty"
	vividName   = "vivid"
)

var (
	defaultOnce sync.Once
	defaultTh   *Theme

	ttyOnce sync.Once
	ttyTh   *Theme

	vividOnce sync.Once
	vividTh   *Theme
)

// defaultColors is btop's built-in Default theme, reproduced verbatim from
// btop_theme.cpp. It is not shipped as a .theme file by btop, so it has to live
// here for rclonetop to look the same out of the box on a host with no themes
// installed.
var defaultColors = map[string]string{
	"main_bg":     "#00",
	"main_fg":     "#cc",
	"title":       "#ee",
	"hi_fg":       "#b54040",
	"selected_bg": "#6a2f2f",
	"selected_fg": "#ee",
	"inactive_fg": "#40",
	"graph_text":  "#60",
	"meter_bg":    "#40",
	"proc_misc":   "#0de756",
	"div_line":    "#30",

	// Box outlines. btop names these after its own panels; rclonetop reuses
	// the palette slots for the panels that occupy the same visual role.
	"cpu_box":  "#556d59",
	"mem_box":  "#6c6c4b",
	"net_box":  "#5c588d",
	"proc_box": "#805252",

	"temp_start": "#4897d4",
	"temp_mid":   "#5474e8",
	"temp_end":   "#ff40b6",

	"cpu_start": "#77ca9b",
	"cpu_mid":   "#cbc06c",
	"cpu_end":   "#dc4c4c",

	"free_start": "#384f21",
	"free_mid":   "#b5e685",
	"free_end":   "#dcff85",

	"cached_start": "#163350",
	"cached_mid":   "#74e6fc",
	"cached_end":   "#26c5ff",

	"available_start": "#4e3f0e",
	"available_mid":   "#ffd77a",
	"available_end":   "#ffb814",

	"used_start": "#592b26",
	"used_mid":   "#d9626d",
	"used_end":   "#ff4769",

	"download_start": "#291f75",
	"download_mid":   "#4f43a3",
	"download_end":   "#b0a9de",

	"upload_start": "#620665",
	"upload_mid":   "#7d4180",
	"upload_end":   "#dcafde",

	"process_start": "#80d0a3",
	"process_mid":   "#dcd179",
	"process_end":   "#d45454",
}

// ttyColors is a palette drawn from the eight ANSI colours, for real consoles
// and for --tty. It is rclonetop's own, not a copy of btop's TTY theme: the
// point is only that every colour survives an 8-colour terminal unchanged.
var ttyColors = map[string]string{
	"main_bg":     "",
	"main_fg":     "#c0c0c0",
	"title":       "#ffffff",
	"hi_fg":       "#ff0000",
	"selected_bg": "#800000",
	"selected_fg": "#ffffff",
	"inactive_fg": "#808080",
	"graph_text":  "#c0c0c0",
	"meter_bg":    "#808080",
	"proc_misc":   "#00ff00",
	"div_line":    "#808080",

	"cpu_box":  "#008000",
	"mem_box":  "#808000",
	"net_box":  "#000080",
	"proc_box": "#800000",

	"temp_start": "#0000ff", "temp_mid": "#800080", "temp_end": "#ff00ff",
	"cpu_start": "#00ff00", "cpu_mid": "#ffff00", "cpu_end": "#ff0000",
	"free_start": "#008000", "free_mid": "#00ff00", "free_end": "#00ff00",
	"cached_start": "#008080", "cached_mid": "#00ffff", "cached_end": "#00ffff",
	"available_start": "#808000", "available_mid": "#ffff00", "available_end": "#ffff00",
	"used_start": "#800000", "used_mid": "#ff0000", "used_end": "#ff0000",
	"download_start": "#000080", "download_mid": "#0000ff", "download_end": "#00ffff",
	"upload_start": "#800080", "upload_mid": "#ff00ff", "upload_end": "#ff00ff",
	"process_start": "#00ff00", "process_mid": "#ffff00", "process_end": "#ff0000",
}

// vividColors is rclonetop's own palette, for a desktop whose terminal theme is
// saturated rather than btop's muted greys: a blue-black background, a cool
// off-white foreground, and each panel outlined in a hue of its own -- pink for
// transfers, blue for bandwidth, green for files, amber for status -- so a
// panel is found by colour before its name is read.
//
// The values are chosen here, not lifted from any other program's palette. The
// ramps keep btop's meaning (cpu and process are green to amber to red, download
// and upload are their own two hues) but start well clear of the background,
// because the point of the theme is that nothing on it is murky; the legibility
// tests in internal/ui hold it to the same floors as the default theme.
//
// meter_bg is the background moved about a sixth of the way towards the
// foreground: dark enough that the track reads as empty, light enough that it
// reads at all, which is what a row of square cells needs to be counted.
var vividColors = map[string]string{
	"main_bg":     "#12131c",
	"main_fg":     "#c8cde0",
	"title":       "#eef0ff",
	"hi_fg":       "#ff5f87",
	"selected_bg": "#2b2d45",
	"selected_fg": "#eef0ff",
	"inactive_fg": "#4c5068",
	"graph_text":  "#6c7190",
	"meter_bg":    "#2c2f42",
	"proc_misc":   "#7bd88f",
	"div_line":    "#2c2f42",

	"proc_box": "#ff7edb",
	"net_box":  "#7aa2ff",
	"mem_box":  "#9ece6a",
	"cpu_box":  "#e0af68",

	"temp_start": "#7aa2ff", "temp_mid": "#bb9af7", "temp_end": "#ff5f87",
	"cpu_start": "#9ece6a", "cpu_mid": "#e0af68", "cpu_end": "#f7768e",
	"free_start": "#5a8a4a", "free_mid": "#9ece6a", "free_end": "#c3f08c",
	"cached_start": "#2f7f9a", "cached_mid": "#7dcfff", "cached_end": "#b4f0ff",
	"available_start": "#8a6a2a", "available_mid": "#e0af68", "available_end": "#ffd28a",
	"used_start": "#8a3a4a", "used_mid": "#f7768e", "used_end": "#ff9eb0",
	"download_start": "#3d59a1", "download_mid": "#7aa2ff", "download_end": "#b4c8ff",
	"upload_start": "#8a3f8a", "upload_mid": "#ff7edb", "upload_end": "#ffb4ec",
	"process_start": "#9ece6a", "process_mid": "#e0af68", "process_end": "#f7768e",
}

// Default returns btop's Default theme.
func Default() *Theme {
	defaultOnce.Do(func() { defaultTh = fromMap(defaultName, defaultColors) })
	return defaultTh
}

// TTY returns the eight-colour fallback theme.
func TTY() *Theme {
	ttyOnce.Do(func() { ttyTh = fromMap(ttyName, ttyColors) })
	return ttyTh
}

// Vivid returns rclonetop's saturated theme.
func Vivid() *Theme {
	vividOnce.Do(func() { vividTh = fromMap(vividName, vividColors) })
	return vividTh
}

// fromMap builds a theme from literal colour values, sharing the gradient
// expansion with the file parser so both paths behave identically.
func fromMap(name string, src map[string]string) *Theme {
	t := &Theme{
		Name:      name,
		colors:    make(map[string]Color, len(src)),
		gradients: make(map[string][]Color, len(GradientNames)),
	}
	for k, v := range src {
		c, err := ParseHex(v)
		if err != nil {
			continue
		}
		t.colors[k] = c
	}
	t.buildGradients()
	return t
}
