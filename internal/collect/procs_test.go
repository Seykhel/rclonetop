package collect

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Seykhel/rclonetop/internal/model"
)

func TestParseCmdline(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantKind    model.Kind
		wantRemotes []string
		wantTarget  string
	}{
		{
			// The real command line of the FUSE mount this collector was
			// written against.
			name: "mount with flags",
			args: []string{
				"/usr/bin/rclone", "mount", "gdrive:", "/home/user/My Drive",
				"--vfs-cache-mode", "full", "--vfs-cache-max-size", "10G",
				"--dir-cache-time", "5m",
			},
			wantKind:    model.KindMount,
			wantRemotes: []string{"gdrive:"},
			wantTarget:  "/home/user/My Drive",
		},
		{
			name: "bisync between local and remote",
			args: []string{
				"rclone", "bisync", "/home/user/Documents", "gdrive:Documents",
				"--check-access", "--max-delete", "25",
			},
			wantKind:    model.KindBisync,
			wantRemotes: []string{"gdrive:Documents"},
			wantTarget:  "gdrive:Documents",
		},
		{
			name:        "sync to a bucket",
			args:        []string{"rclone", "sync", "/home/user/notes", "s3backup:bucket/notes", "--fast-list"},
			wantKind:    model.KindSync,
			wantRemotes: []string{"s3backup:bucket/notes"},
			wantTarget:  "s3backup:bucket/notes",
		},
		{
			// Global flags may precede the subcommand.
			name:        "flags before the subcommand",
			args:        []string{"rclone", "--config", "/tmp/rclone.conf", "rcd", "--rc-no-auth"},
			wantKind:    model.KindRCD,
			wantRemotes: nil,
			wantTarget:  "",
		},
		{
			name:        "unknown subcommand",
			args:        []string{"rclone", "version"},
			wantKind:    model.KindUnknown,
			wantRemotes: nil,
			wantTarget:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, remotes, _, target := parseCmdline(tt.args)
			if kind != tt.wantKind {
				t.Errorf("kind = %q, want %q", kind, tt.wantKind)
			}
			if !reflect.DeepEqual(remotes, tt.wantRemotes) {
				t.Errorf("remotes = %v, want %v", remotes, tt.wantRemotes)
			}
			if target != tt.wantTarget {
				t.Errorf("target = %q, want %q", target, tt.wantTarget)
			}
		})
	}
}

func TestParseRCAddr(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"rclone", "rcd", "--rc-addr", "127.0.0.1:5573"}, "127.0.0.1:5573"},
		{[]string{"rclone", "rcd", "--rc-addr=:5574"}, ":5574"},
		// Both --rc and rcd fall back to rclone's documented default.
		{[]string{"rclone", "sync", "a", "b", "--rc"}, "localhost:5572"},
		{[]string{"rclone", "rcd", "--rc-no-auth"}, "localhost:5572"},
		// A plain mount exposes nothing, and must not be probed.
		{[]string{"rclone", "mount", "gdrive:", "/mnt"}, ""},
	}

	for _, tt := range tests {
		if got := parseRCAddr(tt.args); got != tt.want {
			t.Errorf("parseRCAddr(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestCollectPropagatesObservedRCAddr(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stat"), "btime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (x) R 0\n")
	writeProc(t, root, 42, "rclone", []string{"rclone", "rcd", "--rc-addr", "127.0.0.1:5573"}, "VmRSS:\t 10 kB\n", "rchar: 0\nwchar: 0\n")

	snap, err := NewProcsAt(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Processes) != 1 || snap.Processes[0].RCAddr != "127.0.0.1:5573" {
		t.Fatalf("process RC address = %+v", snap.Processes)
	}
}

// TestCollectFromFixture drives the collector against a fake procfs, so the
// parsing is exercised without depending on what happens to be running.
func TestCollectFromFixture(t *testing.T) {
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "stat"), "cpu  1 2 3\nbtime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (rclonetop) R 0\n")

	// A process that is rclone, and one that is not.
	writeProc(t, root, 2702, "rclone",
		[]string{"/usr/bin/rclone", "mount", "gdrive:", "/home/user/My Drive"},
		"VmRSS:\t   77652 kB\nThreads:\t15\n",
		"rchar: 1000\nwchar: 2000\n")
	writeProc(t, root, 1234, "bash",
		[]string{"/usr/bin/bash"}, "VmRSS:\t 100 kB\nThreads:\t1\n", "rchar: 5\nwchar: 5\n")

	p := NewProcsAt(root)
	if !p.Available() {
		t.Fatal("fixture procfs should be available")
	}

	snap, err := p.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Processes) != 1 {
		t.Fatalf("got %d processes, want 1 (non-rclone processes must be ignored)", len(snap.Processes))
	}

	got := snap.Processes[0]
	if got.PID != 2702 || got.Kind != model.KindMount {
		t.Errorf("got pid %d kind %q", got.PID, got.Kind)
	}
	if got.RSS != 77652*1024 {
		t.Errorf("RSS = %d, want %d (the kernel reports kibibytes)", got.RSS, 77652*1024)
	}
	if got.Threads != 15 {
		t.Errorf("Threads = %d, want 15", got.Threads)
	}
	if !got.IOAvailable || got.ReadTotal != 1000 || got.WriteTotal != 2000 {
		t.Errorf("io = %v read %d write %d", got.IOAvailable, got.ReadTotal, got.WriteTotal)
	}
	// Rates need two samples; the first one has nothing to compare against.
	if got.ReadRate != 0 || got.WriteRate != 0 {
		t.Errorf("first sample must report no rate, got %v/%v", got.ReadRate, got.WriteRate)
	}
}

// The working directory is what a relative path on the command line is
// relative to. Only the process itself knows it, and only while it is alive.
func TestCwdIsRead(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stat"), "btime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (x) R 0\n")
	writeProc(t, root, 42, "rclone",
		[]string{"rclone", "sync", "a", "b", "--log-file", "rclone.log"},
		"VmRSS:\t 10 kB\n", "rchar: 0\nwchar: 0\n")
	if err := os.Symlink("/var/lib/rclone", filepath.Join(root, "42", "cwd")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	snap, err := NewProcsAt(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := snap.Processes[0].Cwd; got != "/var/lib/rclone" {
		t.Errorf("cwd = %q, want /var/lib/rclone", got)
	}
}

// A process owned by another user refuses the link, and an unreadable working
// directory has to stay empty rather than become this process's own.
func TestAnUnreadableCwdIsEmpty(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stat"), "btime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (x) R 0\n")
	writeProc(t, root, 42, "rclone",
		[]string{"rclone", "sync", "a", "b"}, "VmRSS:\t 10 kB\n", "rchar: 0\nwchar: 0\n")

	snap, err := NewProcsAt(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if got := snap.Processes[0].Cwd; got != "" {
		t.Errorf("cwd = %q, want empty", got)
	}
}

// TestRatesNeedASecondSample checks the delta arithmetic, the measurement that
// makes a mount's throughput visible without the rc API.
func TestRatesNeedASecondSample(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stat"), "btime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (x) R 0\n")
	writeProc(t, root, 42, "rclone",
		[]string{"rclone", "sync", "a", "b"}, "VmRSS:\t 10 kB\n", "rchar: 0\nwchar: 0\n")

	p := NewProcsAt(root)
	if _, err := p.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	// Move the counters and rewind the previous sample by exactly one second,
	// so the expected rate is the raw delta.
	writeFile(t, filepath.Join(root, "42", "io"), "rchar: 4096\nwchar: 1024\n")
	prev := p.prev[42]
	prev.at = prev.at.Add(-1)
	p.prev[42] = prev

	snap, err := p.Collect(context.Background())
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	got := snap.Processes[0]
	if got.ReadRate <= 0 || got.WriteRate <= 0 {
		t.Fatalf("expected a positive rate, got read %v write %v", got.ReadRate, got.WriteRate)
	}
	if got.ReadRate < got.WriteRate {
		t.Errorf("read %v should exceed write %v given the counters", got.ReadRate, got.WriteRate)
	}
}

func writeProc(t *testing.T, root string, pid int, comm string, args []string, status, io string) {
	t.Helper()
	dir := filepath.Join(root, itoa(pid))

	writeFile(t, filepath.Join(dir, "comm"), comm+"\n")
	writeFile(t, filepath.Join(dir, "status"), status)
	writeFile(t, filepath.Join(dir, "io"), io)

	// Field 22 is the start time in clock ticks. The parenthesised command
	// name deliberately contains a space, the case that breaks a naive split.
	writeFile(t, filepath.Join(dir, "stat"),
		itoa(pid)+" (some name) S 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 1000 0\n")

	var cmdline []byte
	for _, a := range args {
		cmdline = append(cmdline, []byte(a)...)
		cmdline = append(cmdline, 0)
	}
	writeFile(t, filepath.Join(dir, "cmdline"), string(cmdline))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// TestNoProcessesReportsEmptyNotNil covers the difference between "nothing to
// say" and "looked and found none". A nil slice is left alone by State.Apply,
// which froze the last rclone process on screen forever after it exited.
func TestNoProcessesReportsEmptyNotNil(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "stat"), "btime 1787000000\n")
	writeFile(t, filepath.Join(root, "self", "stat"), "1 (x) R 0\n")
	writeProc(t, root, 99, "bash", []string{"/usr/bin/bash"}, "VmRSS:\t 10 kB\n", "rchar: 0\nwchar: 0\n")

	snap, err := NewProcsAt(root).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap.Processes == nil {
		t.Error("Processes is nil, so the previous frame would never be cleared")
	}
	if len(snap.Processes) != 0 {
		t.Errorf("got %d processes, want none", len(snap.Processes))
	}
}
