package collect

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

// listingSuffix is the extension of a completed bisync listing. The variants
// rclone also writes carry a further suffix: -old is the previous run, -err
// marks a run that did not finish, -dry belongs to a dry run and describes
// nothing that happened.
const listingSuffix = ".lst"

// maxListingBytes caps how much of a single listing is read. A pathological
// listing must not be able to stall the UI or exhaust memory; the cap is far
// above any realistic tree.
const maxListingBytes = 256 << 20

// Bisync reports on rclone bisync sessions by reading the listings it leaves in
// its cache directory.
//
// bisync writes a complete census of both sides after every run:
//
//	# bisync listing v1 from 2026-08-22T18:02:28.967929669+0000
//	-    24494 - - 2026-08-20T11:03:20.000000000+0000 "notes/todo.md"
//
// That is file counts, byte totals and the divergence between the two ends,
// available from local disk with no API calls and no network. It is the only
// source here that can answer "how many files are synchronised and how big are
// they" for a remote whose backend does not implement About -- which is most of
// the interesting ones.
type Bisync struct {
	dir string

	// cache holds the last parse of each file, keyed by path and invalidated
	// on modification time. Listings only change when a run completes, so
	// re-reading them on every tick would be pure waste.
	cache map[string]cachedListing
}

type cachedListing struct {
	modTime time.Time
	size    int64
	listing *listing
}

// listing is one side of a pair as recorded on disk.
type listing struct {
	at    time.Time
	files int
	bytes uint64
	// sizes maps each path to its size, kept so the two sides can be
	// compared. It is the memory cost of knowing the drift.
	sizes map[string]uint64
}

// NewBisync returns a collector reading rclone's default bisync cache
// directory.
func NewBisync() *Bisync { return NewBisyncAt(DefaultBisyncDir()) }

// NewBisyncAt returns a collector reading listings from dir.
func NewBisyncAt(dir string) *Bisync {
	return &Bisync{dir: dir, cache: make(map[string]cachedListing)}
}

func (b *Bisync) Name() string         { return "bisync" }
func (b *Bisync) Source() model.Source { return model.SourceBisync }

// Interval is generous because listings only change when a run finishes, and
// the parse is throttled further by the modification time cache.
func (b *Bisync) Interval() time.Duration { return 30 * time.Second }

func (b *Bisync) Available() bool {
	info, err := os.Stat(b.dir)
	return err == nil && info.IsDir()
}

// DefaultBisyncDir resolves rclone's bisync cache directory the same way rclone
// does: RCLONE_CACHE_DIR wins, then XDG_CACHE_HOME, then ~/.cache.
func DefaultBisyncDir() string {
	if dir := os.Getenv("RCLONE_CACHE_DIR"); dir != "" {
		return filepath.Join(dir, "bisync")
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "rclone", "bisync")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "rclone", "bisync")
}

func (b *Bisync) Collect(ctx context.Context) (model.Snapshot, error) {
	now := time.Now()

	entries, err := os.ReadDir(b.dir)
	if err != nil {
		return model.Snapshot{}, err
	}

	// A session is identified by everything before the ".path1.lst" suffix.
	// Finding the path1 side is enough to discover it; the other files are
	// derived from the same stem.
	var stems []string
	live := make(map[string]bool)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".path1"+listingSuffix) {
			continue
		}
		stems = append(stems, strings.TrimSuffix(name, ".path1"+listingSuffix))
	}
	sort.Strings(stems)

	pairs := make([]model.SyncPair, 0, len(stems))
	for _, stem := range stems {
		if err := ctx.Err(); err != nil {
			return model.Snapshot{}, err
		}
		pair, ok := b.session(stem, live)
		if !ok {
			continue
		}
		pairs = append(pairs, pair)
	}

	// Forget listings that are no longer on disk, so a host that renames or
	// retires sessions does not leak memory.
	for path := range b.cache {
		if !live[path] {
			delete(b.cache, path)
		}
	}

	return model.Snapshot{
		At:        now,
		Source:    model.SourceBisync,
		SyncPairs: pairs,
	}, nil
}

// session assembles one pair from its two listings.
func (b *Bisync) session(stem string, live map[string]bool) (model.SyncPair, bool) {
	path1 := filepath.Join(b.dir, stem+".path1"+listingSuffix)
	path2 := filepath.Join(b.dir, stem+".path2"+listingSuffix)

	left, err := b.load(path1, live)
	if err != nil {
		return model.SyncPair{}, false
	}
	right, err := b.load(path2, live)
	if err != nil {
		// One side without the other still says how much is on the side we
		// can see, which beats hiding the session entirely.
		right = &listing{sizes: map[string]uint64{}}
	}

	leftLabel, rightLabel := splitStem(stem)
	pair := model.SyncPair{
		Name:     stem,
		Left:     model.SyncSide{Label: leftLabel, Files: left.files, Bytes: left.bytes},
		Right:    model.SyncSide{Label: rightLabel, Files: right.files, Bytes: right.bytes},
		Drift:    drift(left, right),
		ListedAt: left.at,
		Source:   model.SourceBisync,
	}

	// bisync leaves a .lst-err behind when a run does not complete. Its
	// modification time is when that happened; the file is not cleaned up, so
	// this is a record of the last failure, not proof of a current one.
	if info, err := os.Stat(path1 + "-err"); err == nil {
		pair.FailedAt = info.ModTime()
	}

	return pair, true
}

// load parses a listing, reusing the previous parse when the file has not
// changed.
func (b *Bisync) load(path string, live map[string]bool) (*listing, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	live[path] = true

	if c, ok := b.cache[path]; ok && c.modTime.Equal(info.ModTime()) && c.size == info.Size() {
		return c.listing, nil
	}
	if info.Size() > maxListingBytes {
		return nil, os.ErrInvalid
	}

	l, err := parseListing(path)
	if err != nil {
		return nil, err
	}
	b.cache[path] = cachedListing{modTime: info.ModTime(), size: info.Size(), listing: l}
	return l, nil
}

// parseListing reads one listing file.
func parseListing(path string) (*listing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	l := &listing{sizes: make(map[string]uint64)}

	sc := bufio.NewScanner(f)
	// Paths can be long; the default 64 KiB line limit is not generous enough
	// for deeply nested trees.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			if at, ok := parseListingHeader(line); ok {
				l.at = at
			}
			continue
		}
		p, size, ok := parseListingLine(line)
		if !ok {
			continue
		}
		l.sizes[p] = size
		l.files++
		l.bytes += size
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return l, nil
}

// headerLayouts are the timestamp formats the listing banner has been seen to
// use.
//
// The zone offset is written without a colon -- "+0000", not "+00:00" -- so it
// is not RFC 3339 despite looking like it, and time.RFC3339Nano rejects it
// outright. Both spellings are accepted here rather than assuming rclone will
// keep writing the current one.
var headerLayouts = []string{
	"2006-01-02T15:04:05.999999999-0700",
	time.RFC3339Nano,
	"2006-01-02T15:04:05-0700",
	time.RFC3339,
}

// parseListingHeader extracts the timestamp from the "# bisync listing v1 from
// <time>" banner.
func parseListingHeader(line string) (time.Time, bool) {
	const marker = " from "
	i := strings.LastIndex(line, marker)
	if i < 0 {
		return time.Time{}, false
	}
	value := strings.TrimSpace(line[i+len(marker):])

	for _, layout := range headerLayouts {
		if at, err := time.Parse(layout, value); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

// parseListingLine reads one entry.
//
// The format is "<flag> <size> <hash> <id> <modtime> \"<path>\"". The path is
// quoted and can contain spaces, so it is taken from the first quote to the
// last rather than by splitting on whitespace.
func parseListingLine(line string) (path string, size uint64, ok bool) {
	open := strings.IndexByte(line, '"')
	if open < 0 {
		return "", 0, false
	}
	close := strings.LastIndexByte(line, '"')
	if close <= open {
		return "", 0, false
	}

	head := strings.Fields(line[:open])
	if len(head) < 2 {
		return "", 0, false
	}
	size, err := strconv.ParseUint(head[1], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return line[open+1 : close], size, true
}

// drift counts the paths the two sides disagree on: missing from one, or
// present on both at different sizes.
func drift(left, right *listing) int {
	n := 0
	for path, size := range left.sizes {
		other, ok := right.sizes[path]
		if !ok || other != size {
			n++
		}
	}
	for path := range right.sizes {
		if _, ok := left.sizes[path]; !ok {
			n++
		}
	}
	return n
}

// splitStem separates the two sides encoded in a listing's filename.
//
// bisync builds the stem by mangling both paths -- slashes, backslashes and
// colons all become underscores -- and joining them with "..". The mangling is
// lossy, so the original paths cannot be recovered and are deliberately not
// guessed at: showing "gdrive/Documents" for what was "gdrive:Documents" would
// be inventing detail. The real paths arrive from the log collector, which sees
// them written out in full.
func splitStem(stem string) (left, right string) {
	i := strings.Index(stem, "..")
	if i < 0 {
		return stem, ""
	}
	return stem[:i], stem[i+2:]
}
