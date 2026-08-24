package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Seykhel/rclonetop/internal/config"
)

// writeConf puts a configuration file in a temporary directory and returns its
// path, so a test can name one through -c without touching the real one.
func writeConf(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), config.Name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigForStopsOnAFileThatWillNotParse(t *testing.T) {
	o := options{configPath: writeConf(t, "update_ms = soon\n")}

	_, err := configFor(o, io.Discard)
	if err == nil {
		t.Fatal("configFor accepted a file it could not parse")
	}
	// The message has to name the file and the line, because it is the only
	// chance the user gets: the alternate screen goes up immediately after.
	if !strings.Contains(err.Error(), config.Name+":1") {
		t.Errorf("configFor = %v, want the file and line of the offending assignment", err)
	}
}

func TestConfigForLetsDebugThroughWithAWarning(t *testing.T) {
	// -d is what users are asked to paste into a bug report, and a host whose
	// configuration file will not parse is exactly the sort that gets reported.
	// If the diagnostic depended on the thing being diagnosed it would be gone
	// when it was most needed.
	o := options{configPath: writeConf(t, "update_ms = soon\n"), debug: true}

	var warn bytes.Buffer
	cfg, err := configFor(o, &warn)
	if err != nil {
		t.Fatalf("configFor refused to run -d: %v", err)
	}
	if cfg != config.Defaults() {
		t.Errorf("configFor = %+v, want the defaults %+v", cfg, config.Defaults())
	}
	// Carrying on in silence would be the worse failure of the two: the dump
	// would look authoritative while none of the user's settings had applied.
	if !strings.Contains(warn.String(), "update_ms") {
		t.Errorf("warning = %q, want it to say which key could not be read", warn.String())
	}
}

func TestConfigForReadsANamedFile(t *testing.T) {
	o := options{configPath: writeConf(t, "update_ms = 750\n")}

	cfg, err := configFor(o, io.Discard)
	if err != nil {
		t.Fatalf("configFor: %v", err)
	}
	if cfg.UpdateMS != 750 {
		t.Errorf("UpdateMS = %d, want 750", cfg.UpdateMS)
	}
}
