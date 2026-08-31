package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseReadsEveryKey(t *testing.T) {
	const file = `
color_theme = "dracula"
theme_background = False
graph_symbol = "block"
update_ms = 500
base_10_sizes = True
force_tty = True
truecolor = False
clock_layout = "15:04"
shown_boxes = "transfers status"
preset_1 = "transfers:0:2 bandwidth:1:1"
preset_9 = "status:1:3"
`
	got, err := parse("test.conf", strings.NewReader(file))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Config{
		ColorTheme:      "dracula",
		ThemeBackground: false,
		GraphSymbol:     "block",
		UpdateMS:        500,
		Base10Sizes:     true,
		ForceTTY:        true,
		TrueColor:       false,
		ClockLayout:     "15:04",
		ShownBoxes:      "transfers status",
		Preset1:         "transfers:0:2 bandwidth:1:1",
		Preset9:         "status:1:3",
	}
	if got != want {
		t.Errorf("parse = %+v, want %+v", got, want)
	}
}

func TestParseKeepsDefaultsForAbsentKeys(t *testing.T) {
	// A file that mentions one key must not blank out the rest: the file is
	// read over the top of the defaults, not in place of them.
	got, err := parse("test.conf", strings.NewReader("update_ms = 5000\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := Defaults()
	want.UpdateMS = 5000
	if got != want {
		t.Errorf("parse = %+v, want %+v", got, want)
	}
}

func TestParseIgnoresCommentsAndBlankLines(t *testing.T) {
	const file = `
#? Configuration file for rclonetop

#* The colour theme.
color_theme = "gruvbox_dark"

   # indented comment
`
	got, err := parse("test.conf", strings.NewReader(file))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ColorTheme != "gruvbox_dark" {
		t.Errorf("ColorTheme = %q, want gruvbox_dark", got.ColorTheme)
	}
}

func TestParseIgnoresUnknownKeys(t *testing.T) {
	// A configuration file written for a later rclonetop names boxes and
	// presets this build has never heard of. Refusing to start on that would
	// make every downgrade -- and every shared dotfile repository -- a failure,
	// so an unrecognised key is skipped rather than rejected.
	const file = `
shown_boxes = "transfers remotes bandwidth"
presets = "transfers:0:default"
vim_keys = True
color_theme = "dracula"
`
	got, err := parse("test.conf", strings.NewReader(file))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ColorTheme != "dracula" {
		t.Errorf("ColorTheme = %q: a later version's keys stopped the ones this build knows from being read", got.ColorTheme)
	}
}

func TestParseAcceptsBothBooleanSpellings(t *testing.T) {
	// btop writes True and False; anyone editing the file by hand is as likely
	// to type the lowercase spelling every other configuration format uses.
	for _, spelling := range []string{"True", "true", "TRUE"} {
		got, err := parse("test.conf", strings.NewReader("base_10_sizes = "+spelling+"\n"))
		if err != nil {
			t.Fatalf("parse(%s): %v", spelling, err)
		}
		if !got.Base10Sizes {
			t.Errorf("parse(%s): Base10Sizes = false, want true", spelling)
		}
	}
	for _, spelling := range []string{"False", "false", "FALSE"} {
		got, err := parse("test.conf", strings.NewReader("theme_background = "+spelling+"\n"))
		if err != nil {
			t.Fatalf("parse(%s): %v", spelling, err)
		}
		if got.ThemeBackground {
			t.Errorf("parse(%s): ThemeBackground = true, want false", spelling)
		}
	}
}

func TestParseAcceptsUnquotedStrings(t *testing.T) {
	got, err := parse("test.conf", strings.NewReader("color_theme = dracula\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ColorTheme != "dracula" {
		t.Errorf("ColorTheme = %q, want dracula", got.ColorTheme)
	}
}

func TestParseAcceptsAnUnknownGraphSymbol(t *testing.T) {
	// The same forward compatibility as an unknown key, one level down: the
	// plotter falls back to braille on a symbol it does not recognise, so a
	// value from a later version costs a different-looking graph rather than a
	// refusal to start. The command line is stricter on purpose -- a typo typed
	// at the prompt wants an answer now.
	got, err := parse("test.conf", strings.NewReader("graph_symbol = \"hexagon\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.GraphSymbol != "hexagon" {
		t.Errorf("GraphSymbol = %q, want it passed through untouched", got.GraphSymbol)
	}
}

func TestParseAcceptsAnUnknownShownBoxesName(t *testing.T) {
	// This package has no reason to know the panel vocabulary, so it neither
	// validates nor drops a name -- internal/ui does that, the same split
	// already drawn for graph_symbol.
	got, err := parse("test.conf", strings.NewReader("shown_boxes = \"transfers remotes\"\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ShownBoxes != "transfers remotes" {
		t.Errorf("ShownBoxes = %q, want it passed through untouched", got.ShownBoxes)
	}
}

func TestParseReadsPresetKeys(t *testing.T) {
	got, err := parse("test.conf", strings.NewReader(`preset_2 = "transfers:0:2 status:1:1"`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Preset2 != "transfers:0:2 status:1:1" {
		t.Errorf("Preset2 = %q", got.Preset2)
	}
}

func TestParseRejectsInvalidPresetEntries(t *testing.T) {
	for _, value := range []string{"bogus:0:1", "files:2:1", "status:1:0", "status:1:one", "status:1:1 status:0:2", "status:1"} {
		_, err := parse("test.conf", strings.NewReader("preset_2 = \""+value+"\""))
		if err == nil {
			t.Errorf("parse accepted invalid preset %q", value)
		}
		if !strings.Contains(err.Error(), "test.conf:1") {
			t.Errorf("error %v lacks file and line", err)
		}
	}
}

func TestParseRejectsPresetNumbersOutsideRange(t *testing.T) {
	for _, key := range []string{"preset_0", "preset_10"} {
		_, err := parse("test.conf", strings.NewReader(key+" = \"\""))
		if err == nil {
			t.Errorf("parse accepted %s", key)
		}
		if !strings.Contains(err.Error(), "between 1 and 9") {
			t.Errorf("error %v lacks range", err)
		}
	}
}

func TestParseRejectsMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string // a fragment the message must contain
	}{
		{"not a number", "update_ms = soon\n", "update_ms"},
		{"out of range", "update_ms = 10\n", "at least 100"},
		{"not a boolean", "theme_background = maybe\n", "theme_background"},
		{"no equals sign", "color_theme dracula\n", "expected key = value"},
		// strftime is what btop's clock_format takes and what a user copying
		// from btop.conf will paste. Rendering "%X" literally in the header
		// would look like rclonetop was broken, so say what happened instead.
		{"strftime clock layout", "clock_layout = \"%X\"\n", "reference layout"},
		// btop's own spelling is the one unknown key that is refused rather
		// than skipped: it will never be a later version's key, so silence
		// would just leave the mistake to be found on screen.
		{"btop's spelling of the clock", "clock_format = \"%X\"\n", "clock_layout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parse("test.conf", strings.NewReader(tt.file))
			if err == nil {
				t.Fatalf("parse(%q) accepted the input", tt.file)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parse(%q) = %v, want a message mentioning %q", tt.file, err, tt.want)
			}
		})
	}
}

func TestParseErrorNamesFileAndLine(t *testing.T) {
	const file = `color_theme = "dracula"

update_ms = soon
`
	_, err := parse("rclonetop.conf", strings.NewReader(file))
	if err == nil {
		t.Fatal("parse accepted a malformed update_ms")
	}
	if !strings.Contains(err.Error(), "rclonetop.conf:3") {
		t.Errorf("parse = %v, want the file and line of the offending assignment", err)
	}
}

func TestDefaultFileRoundTrips(t *testing.T) {
	// The single fact that keeps --default-config honest. It is the output a
	// user is told to redirect into their configuration directory, so it has to
	// parse, and every value it prints has to be the value rclonetop would have
	// used had the file not existed. Nothing else pins the two together: the
	// text is written by hand.
	got, err := parse("default", strings.NewReader(DefaultFile("0.1.0-test")))
	if err != nil {
		t.Fatalf("the output of --default-config does not parse: %v", err)
	}
	if got != Defaults() {
		t.Errorf("--default-config prints %+v, but the defaults are %+v", got, Defaults())
	}
}

func TestDefaultFileDocumentsEveryKey(t *testing.T) {
	// A key the file does not mention is a key nobody will discover: this is
	// the only documentation of the format that ships with the binary.
	keys := []string{
		"color_theme", "theme_background", "graph_symbol",
		"update_ms", "base_10_sizes", "force_tty", "truecolor", "clock_layout",
		"shown_boxes",
		"preset_1", "preset_2", "preset_3", "preset_4", "preset_5", "preset_6", "preset_7", "preset_8", "preset_9",
	}
	// The list above is written out by hand, so a field added to Config would
	// otherwise slip past both this test and the file it checks -- the round
	// trip only pins the values of keys that are already printed. Counting the
	// fields is what makes the omission fail here rather than in the wild.
	if n := reflect.TypeOf(Config{}).NumField(); n != len(keys) {
		t.Fatalf("Config has %d fields but this test names %d keys: add the new one here, and to defaultFile", n, len(keys))
	}

	file := DefaultFile("0.1.0-test")
	for _, key := range keys {
		if !strings.Contains(file, "\n"+key+" = ") {
			t.Errorf("--default-config never assigns %s", key)
		}
	}
}

func TestLoadNamedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rclonetop.conf")
	if err := os.WriteFile(path, []byte("update_ms = 750\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UpdateMS != 750 {
		t.Errorf("UpdateMS = %d, want 750", got.UpdateMS)
	}
}

func TestLoadMissingNamedFileIsAnError(t *testing.T) {
	// Silently falling back to the defaults here would hide a typo in the path
	// the user typed, and they would spend the session wondering why none of
	// their settings took.
	if _, err := Load(filepath.Join(t.TempDir(), "absent.conf")); err == nil {
		t.Error("Load accepted a path that does not exist")
	}
}

func TestLoadWithoutAFileIsTheDefaults(t *testing.T) {
	// The ordinary case: nobody has written a configuration file, and that is
	// not a problem to report.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Defaults() {
		t.Errorf("Load = %+v, want the defaults %+v", got, Defaults())
	}
}

func TestLoadPrefersXDGOverHome(t *testing.T) {
	xdg, home := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)

	write := func(root, body string) {
		dir := filepath.Join(root, "rclonetop")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rclonetop.conf"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(xdg, "color_theme = \"from-xdg\"\n")
	write(filepath.Join(home, ".config"), "color_theme = \"from-home\"\n")

	got, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ColorTheme != "from-xdg" {
		t.Errorf("ColorTheme = %q, want from-xdg", got.ColorTheme)
	}
}
