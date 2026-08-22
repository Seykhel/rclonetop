package theme

import "testing"

func TestParseHex(t *testing.T) {
	tests := []struct {
		in      string
		want    Color
		wantErr bool
	}{
		{"#282a36", Color{0x28, 0x2a, 0x36, true}, false},
		// Two digits are btop's greyscale shorthand, replicated across all
		// three channels. Themes use it heavily for neutral greys.
		{"#cc", Color{0xcc, 0xcc, 0xcc, true}, false},
		{"#00", Color{0x00, 0x00, 0x00, true}, false},
		// An empty value means "inherit from the terminal", not black.
		{"", Color{}, false},
		// Three-digit CSS shorthand is not part of the format.
		{"#abc", Color{}, true},
		{"282a36", Color{}, true},
	}

	for _, tt := range tests {
		got, err := ParseHex(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseHex(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("ParseHex(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseThemeFile(t *testing.T) {
	// A slice of a real btop theme, including a comment and a blank line.
	lines := []string{
		"#Dracula theme",
		"",
		`theme[main_bg]="#282a36"`,
		`theme[main_fg]="#f8f8f2"`,
		`theme[download_start]="#000000"`,
		`theme[download_mid]="#808080"`,
		`theme[download_end]="#ffffff"`,
	}
	th := parse("test", lines)

	if got := th.Color("main_bg"); got != (Color{0x28, 0x2a, 0x36, true}) {
		t.Errorf("main_bg = %+v", got)
	}

	// The ramp must hit its three anchors exactly, or the colours drift away
	// from what the theme author chose.
	for _, tt := range []struct {
		frac float64
		want uint8
	}{{0, 0x00}, {0.5, 0x80}, {1, 0xff}} {
		if got := th.Gradient("download", tt.frac); got.R != tt.want {
			t.Errorf("Gradient(download, %v).R = %#x, want %#x", tt.frac, got.R, tt.want)
		}
	}

	// Out-of-range fractions clamp rather than panic: callers pass raw
	// ratios and a burst above the observed peak is normal.
	if got := th.Gradient("download", 5); got.R != 0xff {
		t.Errorf("Gradient(download, 5).R = %#x, want 0xff", got.R)
	}
	if got := th.Gradient("download", -1); got.R != 0x00 {
		t.Errorf("Gradient(download, -1).R = %#x, want 0x00", got.R)
	}
}

func TestMissingKeyFallsBackToDefault(t *testing.T) {
	// Themes written for older btop versions omit newer keys; they must not
	// render as an unset colour on top of an unset background.
	th := parse("sparse", []string{`theme[main_fg]="#ffffff"`})
	if got := th.Color("proc_misc"); !got.Set {
		t.Error("missing key should fall back to the built-in theme")
	}
	if got := th.Gradient("cpu", 0.5); !got.Set {
		t.Error("missing gradient should fall back to the built-in theme")
	}
}

func TestSetOpaqueBackground(t *testing.T) {
	th := parse("test", []string{`theme[main_bg]="#282a36"`})
	th.SetOpaqueBackground(false)
	if th.colors["main_bg"].Set {
		t.Error("a transparent background must leave main_bg unset")
	}
}

func TestDefaultThemeIsComplete(t *testing.T) {
	d := Default()
	for _, name := range GradientNames {
		if got := d.Gradient(name, 0.5); !got.Set {
			t.Errorf("default theme is missing the %q gradient", name)
		}
	}
	for _, key := range []string{"main_fg", "title", "hi_fg", "inactive_fg", "div_line", "proc_misc"} {
		if got := d.Color(key); !got.Set {
			t.Errorf("default theme is missing %q", key)
		}
	}
}
