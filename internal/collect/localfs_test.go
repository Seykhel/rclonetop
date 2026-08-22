package collect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseMountInfo(t *testing.T) {
	// Real lines, trimmed. The rclone mount deliberately has a space in its
	// mountpoint, which the kernel escapes as \040 -- the case that breaks a
	// naive field split.
	content := `25 30 0:23 / /sys rw,nosuid,nodev,noexec,relatime shared:7 - sysfs sysfs rw
30 1 0:26 / / rw,relatime shared:1 - btrfs /dev/nvme0n1p3 rw,ssd,subvol=/root
95 30 0:52 / /home/user/My\040Drive rw,nosuid,nodev,relatime shared:60 - fuse.rclone gdrive: rw,user_id=1000,group_id=1000
99 30 0:55 / /home/user/pCloudDrive rw,nosuid,nodev,relatime - fuse pCloud.fs rw,user_id=1000
`
	path := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mounts, err := parseMountInfo(path)
	if err != nil {
		t.Fatalf("parseMountInfo: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("got %d mounts, want 1 (only fuse.rclone counts)", len(mounts))
	}

	got := mounts[0]
	if got.Mountpoint != "/home/user/My Drive" {
		t.Errorf("mountpoint = %q, want %q", got.Mountpoint, "/home/user/My Drive")
	}
	if got.Remote != "gdrive:" {
		t.Errorf("remote = %q, want %q", got.Remote, "gdrive:")
	}
	if got.FSType != "fuse.rclone" {
		t.Errorf("fstype = %q", got.FSType)
	}
}

func TestParseMountInfoWithOptionalFields(t *testing.T) {
	// The optional fields before " - " are variable in number, including none
	// at all, so the separator has to drive the split.
	content := `95 30 0:52 / /mnt/a rw,relatime - fuse.rclone one: rw
96 30 0:53 / /mnt/b rw,relatime shared:60 master:2 propagate_from:1 - fuse.rclone two: rw
`
	path := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mounts, err := parseMountInfo(path)
	if err != nil {
		t.Fatalf("parseMountInfo: %v", err)
	}
	if len(mounts) != 2 {
		t.Fatalf("got %d mounts, want 2", len(mounts))
	}
	if mounts[0].Mountpoint != "/mnt/a" || mounts[1].Mountpoint != "/mnt/b" {
		t.Errorf("mountpoints = %q, %q", mounts[0].Mountpoint, mounts[1].Mountpoint)
	}
}

func TestUnescapeMountField(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/home/user/My\\040Drive", "/home/user/My Drive"},
		{"/plain/path", "/plain/path"},
		{"tab\\011here", "tab\there"},
		// A backslash that is not a complete octal escape is left alone.
		{"trailing\\", "trailing\\"},
		{"not\\09xoctal", "not\\09xoctal"},
	}
	for _, tt := range tests {
		if got := unescapeMountField(tt.in); got != tt.want {
			t.Errorf("unescapeMountField(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScanCaches(t *testing.T) {
	root := t.TempDir()
	vfs := filepath.Join(root, "vfs", "gdrive", "sub")
	if err := os.MkdirAll(vfs, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(vfs, name), make([]byte, 4096), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// vfsMeta is absent, and an absent cache directory must simply not appear.

	l := NewLocalFSAt(filepath.Join(root, "no-such-mountinfo"), root)
	caches := l.scanCaches(context.Background(), timeNowForTest())

	if len(caches) != 1 {
		t.Fatalf("got %d cache dirs, want 1", len(caches))
	}
	if caches[0].Kind != "vfs" {
		t.Errorf("kind = %q", caches[0].Kind)
	}
	if caches[0].Files != 3 {
		t.Errorf("files = %d, want 3", caches[0].Files)
	}
	// Disk usage is counted in allocated blocks, so it is at least the
	// apparent size and never zero for non-empty files.
	if caches[0].Bytes < 3*4096 {
		t.Errorf("bytes = %d, want at least %d", caches[0].Bytes, 3*4096)
	}
}

func timeNowForTest() time.Time { return time.Unix(1787000000, 0) }
