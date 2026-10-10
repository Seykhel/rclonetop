package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Seykhel/rclonetop/internal/collect"
	"github.com/Seykhel/rclonetop/internal/config"
	"github.com/Seykhel/rclonetop/internal/model"
	"github.com/Seykhel/rclonetop/internal/ui"
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

func TestMonitorNormalQuit(t *testing.T) {
	for _, key := range []string{"q", "\x03", "\x1b"} {
		t.Run(fmtKey(key), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := runMonitor(ctx, nil, ui.Options{}, tea.WithInput(strings.NewReader(key)), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
			if err != nil {
				t.Fatalf("normal quit: %v", err)
			}
		})
	}
}
func fmtKey(key string) string {
	switch key {
	case "\x03":
		return "ctrl+c"
	case "\x1b":
		return "escape"
	default:
		return key
	}
}

// A collector blocked on host I/O must be released by every exit path.
type waitingCollector struct{ stopped chan struct{} }

func (c waitingCollector) Name() string            { return "waiting" }
func (c waitingCollector) Source() model.Source    { return model.SourceProc }
func (c waitingCollector) Interval() time.Duration { return time.Hour }
func (c waitingCollector) Available() bool         { return true }
func (c waitingCollector) Collect(ctx context.Context) (model.Snapshot, error) {
	<-ctx.Done()
	close(c.stopped)
	return model.Snapshot{}, ctx.Err()
}

func TestMonitorStopsCollectors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := waitingCollector{stopped: make(chan struct{})}
	err := runMonitor(ctx, []collect.Collector{c}, ui.Options{}, tea.WithInput(strings.NewReader("q")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.stopped:
	case <-ctx.Done():
		t.Fatal("collector did not stop on quit")
	}
}

func TestMonitorRequestedCancellationSucceeds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runMonitor(ctx, nil, ui.Options{}, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if err != nil {
		t.Fatalf("requested cancellation: %v", err)
	}
}

type failedInput struct{ err error }

func (r failedInput) Read([]byte) (int, error) { return 0, r.err }

func TestMonitorPreservesInputErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	failure := errors.New("terminal input failed")
	err := runMonitor(ctx, nil, ui.Options{}, tea.WithInput(failedInput{failure}), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if !errors.Is(err, failure) {
		t.Fatalf("Run = %v, want input failure", err)
	}
}

func TestMonitorPreservesDeadlineErrors(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	err := runMonitor(ctx, nil, ui.Options{}, tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want deadline failure", err)
	}
}
