package collect

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseListingLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantPath string
		wantSize uint64
		wantOK   bool
	}{
		{
			name:     "ordinary entry",
			line:     `-    24494 - - 2026-08-20T11:03:20.000000000+0000 "notes/todo.md"`,
			wantPath: "notes/todo.md",
			wantSize: 24494,
			wantOK:   true,
		},
		{
			// Paths routinely contain spaces, which is why the path is taken
			// from the first quote to the last rather than by field splitting.
			name:     "path with spaces",
			line:     `-      512 - - 2026-08-20T11:03:20.000000000+0000 "00-09 System/00.01 routing notes.md"`,
			wantPath: "00-09 System/00.01 routing notes.md",
			wantSize: 512,
			wantOK:   true,
		},
		{
			name:     "empty file",
			line:     `-        0 - - 2021-11-22T14:19:41.000000000+0000 ".localized"`,
			wantPath: ".localized",
			wantSize: 0,
			wantOK:   true,
		},
		{name: "header", line: "# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000"},
		{name: "blank", line: ""},
		{name: "unquoted", line: "- 100 - - 2026-08-20T11:03:20.000000000+0000 notes.md"},
		{name: "size not a number", line: `- xyz - - t "notes.md"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, size, ok := parseListingLine(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && (path != tt.wantPath || size != tt.wantSize) {
				t.Errorf("got (%q, %d), want (%q, %d)", path, size, tt.wantPath, tt.wantSize)
			}
		})
	}
}

func TestParseListingHeader(t *testing.T) {
	// The zone offset has no colon, so this is not RFC 3339 despite looking
	// like it. Parsing it with time.RFC3339Nano fails silently and the run
	// timestamp disappears from the display.
	got, ok := parseListingHeader("# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000")
	if !ok {
		t.Fatal("failed to parse a header with a colonless zone offset")
	}
	want := time.Date(2026, 8, 22, 18, 2, 28, 967929669, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}

	// The RFC 3339 spelling must keep working if rclone ever switches to it.
	if _, ok := parseListingHeader("# bisync listing v1 from 2026-08-22T18:02:28.967929669+00:00"); !ok {
		t.Error("failed to parse a header with an RFC 3339 zone offset")
	}
	if _, ok := parseListingHeader("# something else entirely"); ok {
		t.Error("an unrelated comment should not parse as a header")
	}
}

func TestSplitStem(t *testing.T) {
	left, right := splitStem("home_user_Documents..gdrive_Documents")
	if left != "home_user_Documents" || right != "gdrive_Documents" {
		t.Errorf("got (%q, %q)", left, right)
	}
	if l, r := splitStem("nodelimiter"); l != "nodelimiter" || r != "" {
		t.Errorf("got (%q, %q)", l, r)
	}
}

func TestBisyncCollect(t *testing.T) {
	dir := t.TempDir()
	stem := "home_user_Documents..gdrive_Documents"

	writeListing(t, filepath.Join(dir, stem+".path1.lst"),
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
		`-      200 - - 2026-08-20T11:03:20.000000000+0000 "dir/b with space.md"`,
		`-      300 - - 2026-08-20T11:03:20.000000000+0000 "only-on-the-left.md"`,
	)
	writeListing(t, filepath.Join(dir, stem+".path2.lst"),
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
		`-      999 - - 2026-08-20T11:03:20.000000000+0000 "dir/b with space.md"`,
	)

	b := NewBisyncAt(dir)
	if !b.Available() {
		t.Fatal("fixture directory should be available")
	}

	snap, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.SyncPairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(snap.SyncPairs))
	}

	p := snap.SyncPairs[0]
	if p.Left.Files != 3 || p.Left.Bytes != 600 {
		t.Errorf("left = %d files %d bytes, want 3/600", p.Left.Files, p.Left.Bytes)
	}
	if p.Right.Files != 2 || p.Right.Bytes != 1099 {
		t.Errorf("right = %d files %d bytes, want 2/1099", p.Right.Files, p.Right.Bytes)
	}
	// One path is missing on the right, one differs in size: two disagreements.
	if p.Drift != 2 {
		t.Errorf("drift = %d, want 2", p.Drift)
	}
	if p.ListedAt.IsZero() {
		t.Error("the listing timestamp should have been parsed")
	}
	if !p.FailedAt.IsZero() {
		t.Error("no .lst-err is present, so no failure should be reported")
	}
	if p.Left.Label != "home_user_Documents" || p.Right.Label != "gdrive_Documents" {
		t.Errorf("labels = %q / %q", p.Left.Label, p.Right.Label)
	}
}

// The listing filename is all the bisync collector has, and it is lossy: every
// slash, colon and space in both paths became an underscore. The log collector
// sees the paths written out in full and hands them over here.
//
// The stem below is exactly what rclone produced for "/tmp/.../My Docs" in a
// real run, with the path neutralised: the space is mangled the same way the
// separators are.
func TestPathsFromTheLogAreAttachedToTheirSession(t *testing.T) {
	dir := t.TempDir()
	stem := "home_user_My_Documents..gdrive_Documents"
	writeListing(t, filepath.Join(dir, stem+".path1.lst"),
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
	)
	writeListing(t, filepath.Join(dir, stem+".path2.lst"),
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
	)

	b := NewBisyncAt(dir)
	b.NotePaths("/home/user/My Documents/", "gdrive:Documents/")

	snap, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	p := snap.SyncPairs[0]
	if p.Left.Path != "/home/user/My Documents/" || p.Right.Path != "gdrive:Documents/" {
		t.Errorf("paths = %q / %q, want the ones from the log", p.Left.Path, p.Right.Path)
	}
	// The mangled labels stay: they are what is on disk, and the display falls
	// back to them for a session no log has described.
	if p.Left.Label != "home_user_My_Documents" {
		t.Errorf("label = %q, want it left alone", p.Left.Label)
	}
}

// A pair of paths that does not canonicalise to this session's name describes a
// different session. Attaching it anyway would put one job's paths against
// another job's file counts.
func TestPathsFromAnotherSessionAreNotAttached(t *testing.T) {
	dir := t.TempDir()
	stem := "home_user_Documents..gdrive_Documents"
	writeListing(t, filepath.Join(dir, stem+".path1.lst"),
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
	)
	writeListing(t, filepath.Join(dir, stem+".path2.lst"),
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
	)

	b := NewBisyncAt(dir)
	b.NotePaths("/home/user/Pictures/", "gdrive:Pictures/")

	snap, _ := b.Collect(context.Background())
	if got := snap.SyncPairs[0].Left.Path; got != "" {
		t.Errorf("path = %q, want none: the log described a different pair", got)
	}
}

// NotePaths is called from the log collector's goroutine while Collect runs on
// this one. Without the lock Go aborts the program on the concurrent map
// access, which takes the whole display down with it.
func TestConcurrentNotePathsAndCollect(t *testing.T) {
	dir := t.TempDir()
	stem := "home_user_Documents..gdrive_Documents"
	writeListing(t, filepath.Join(dir, stem+".path1.lst"),
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
	)

	b := NewBisyncAt(dir)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			b.NotePaths("/home/user/Documents/", "gdrive:Documents/")
		}
	}()
	for i := 0; i < 200; i++ {
		if _, err := b.Collect(context.Background()); err != nil {
			t.Errorf("Collect: %v", err)
			break
		}
	}
	<-done
}

func TestBisyncReportsLastFailure(t *testing.T) {
	dir := t.TempDir()
	stem := "a..b"

	writeListing(t, filepath.Join(dir, stem+".path1.lst"),
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`)
	writeListing(t, filepath.Join(dir, stem+".path2.lst"),
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`)
	// bisync leaves this behind when a run does not finish cleanly.
	writeListing(t, filepath.Join(dir, stem+".path1.lst-err"), "# partial")

	snap, err := NewBisyncAt(dir).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if snap.SyncPairs[0].FailedAt.IsZero() {
		t.Error("a .lst-err file should be reported as the last failure")
	}
	if snap.SyncPairs[0].Drift != 0 {
		t.Error("identical listings should show no drift")
	}
}

// TestBisyncCachesByModTime checks that an unchanged listing is not re-parsed.
// Listings only change when a run completes, so re-reading megabytes on every
// tick would be pure waste.
func TestBisyncCachesByModTime(t *testing.T) {
	dir := t.TempDir()
	stem := "a..b"
	path1 := filepath.Join(dir, stem+".path1.lst")

	writeListing(t, path1,
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`)
	writeListing(t, filepath.Join(dir, stem+".path2.lst"),
		"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`)

	b := NewBisyncAt(dir)
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	first := b.cache[path1].listing

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if b.cache[path1].listing != first {
		t.Error("an unchanged listing was parsed again instead of being reused")
	}

	// Changing the file must invalidate the entry.
	writeListing(t, path1,
		"# bisync listing v1 from 2026-08-22T19:02:28.967929669+0000",
		`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`,
		`-      200 - - 2026-08-20T11:03:20.000000000+0000 "b.md"`)
	if err := os.Chtimes(path1, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("third Collect: %v", err)
	}
	if b.cache[path1].listing == first {
		t.Error("a modified listing should have been re-parsed")
	}
	if got := b.cache[path1].listing.files; got != 2 {
		t.Errorf("re-parsed listing has %d files, want 2", got)
	}
}

// TestBisyncIgnoresVariantListings checks that the -old, -err and -dry files
// are not mistaken for sessions of their own.
func TestBisyncIgnoresVariantListings(t *testing.T) {
	dir := t.TempDir()
	stem := "a..b"

	for _, suffix := range []string{".path1.lst", ".path2.lst"} {
		writeListing(t, filepath.Join(dir, stem+suffix),
			"# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000",
			`-      100 - - 2026-08-20T11:03:20.000000000+0000 "a.md"`)
	}
	for _, suffix := range []string{".path1.lst-old", ".path1.lst-err", ".path1.lst-dry", ".path1.lst-dry-old"} {
		writeListing(t, filepath.Join(dir, stem+suffix), "# noise")
	}

	snap, err := NewBisyncAt(dir).Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.SyncPairs) != 1 {
		t.Fatalf("got %d pairs, want 1", len(snap.SyncPairs))
	}
}

func writeListing(t *testing.T, path string, lines ...string) {
	t.Helper()
	content := ""
	for _, l := range lines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
