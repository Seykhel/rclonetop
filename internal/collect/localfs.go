package collect

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Seykhel/rclonetop/internal/model"
)

// cacheRescan is how often the cache directories are walked. Counting bytes
// means stat-ing every file, which is the most expensive thing rclonetop does,
// so it happens far less often than the rest and never on the render path.
const cacheRescan = time.Minute

// cacheKinds are the subdirectories under rclone's cache root worth measuring.
// vfs holds the file data a mount has pulled down, vfsMeta its bookkeeping.
var cacheKinds = []string{"vfs", "vfsMeta"}

// LocalFS reports the local footprint of rclone: the FUSE mounts it is serving
// and the disk its caches occupy.
//
// Both answer "space used" from the side that can actually be measured. A
// remote's own usage needs the About feature, which many backends -- S3 and
// several consumer drives among them -- simply do not implement.
type LocalFS struct {
	mountInfo string // /proc/self/mountinfo, overridable for tests
	cacheRoot string

	lastScan time.Time
	caches   []model.CacheDir
}

// NewLocalFS returns a collector reading the live system.
func NewLocalFS() *LocalFS {
	return &LocalFS{
		mountInfo: "/proc/self/mountinfo",
		cacheRoot: DefaultCacheRoot(),
	}
}

// NewLocalFSAt returns a collector reading a specific mountinfo file and cache
// root, so tests need neither.
func NewLocalFSAt(mountInfo, cacheRoot string) *LocalFS {
	return &LocalFS{mountInfo: mountInfo, cacheRoot: cacheRoot}
}

func (l *LocalFS) Name() string            { return "localfs" }
func (l *LocalFS) Source() model.Source    { return model.SourceLocalFS }
func (l *LocalFS) Interval() time.Duration { return 5 * time.Second }

func (l *LocalFS) Available() bool {
	_, err := os.Stat(l.mountInfo)
	return err == nil
}

// DefaultCacheRoot resolves rclone's cache directory, following the same
// precedence rclone itself uses.
func DefaultCacheRoot() string {
	if dir := os.Getenv("RCLONE_CACHE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "rclone")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "rclone")
}

func (l *LocalFS) Collect(ctx context.Context) (model.Snapshot, error) {
	now := time.Now()

	mounts, err := parseMountInfo(l.mountInfo)
	if err != nil {
		return model.Snapshot{}, err
	}

	// The cache walk runs on its own much slower clock. Between walks the
	// previous figures are reported unchanged, carrying the timestamp of when
	// they were taken so the UI can say how old they are.
	if l.cacheRoot != "" && now.Sub(l.lastScan) >= cacheRescan {
		l.caches = l.scanCaches(ctx, now)
		l.lastScan = now
	}

	return model.Snapshot{
		At:     now,
		Source: model.SourceLocalFS,
		Mounts: mounts,
		Caches: l.caches,
	}, nil
}

// scanCaches measures each cache subdirectory that exists.
func (l *LocalFS) scanCaches(ctx context.Context, now time.Time) []model.CacheDir {
	caches := make([]model.CacheDir, 0, len(cacheKinds))
	for _, kind := range cacheKinds {
		path := filepath.Join(l.cacheRoot, kind)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		bytes, files := dirSize(ctx, path)
		caches = append(caches, model.CacheDir{
			Kind:      kind,
			Path:      path,
			Bytes:     bytes,
			Files:     files,
			ScannedAt: now,
		})
	}
	return caches
}

// dirSize walks a directory tree, summing the disk space its files occupy.
//
// It counts allocated blocks rather than apparent sizes, which is what "space
// used" means and what du reports. The two diverge sharply in exactly the place
// that matters here: rclone's vfsMeta directory holds hundreds of tiny files,
// whose apparent total is a small fraction of the blocks they actually pin.
//
// Unreadable entries are skipped rather than aborting the walk: a partial
// figure is more useful than none, and a cache directory can easily contain
// something this process may not stat.
func dirSize(ctx context.Context, root string) (bytes uint64, files int) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		bytes += diskUsage(info)
		files++
		return nil
	})
	return bytes, files
}

// diskUsage reports the space a file occupies, falling back to its apparent
// size when the block count is unavailable.
func diskUsage(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Blocks >= 0 {
		// st_blocks is fixed at 512-byte units regardless of the
		// filesystem's own block size.
		return uint64(st.Blocks) * 512
	}
	if info.Size() < 0 {
		return 0
	}
	return uint64(info.Size())
}

// parseMountInfo finds the rclone FUSE mounts in /proc/self/mountinfo.
//
// The format puts a variable number of optional fields before a " - "
// separator, so the line is split on that separator rather than by counting
// fields from the left.
func parseMountInfo(path string) ([]model.Mount, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mounts := []model.Mount{}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}

		before := strings.Fields(line[:sep])
		after := strings.Fields(line[sep+3:])
		if len(before) < 5 || len(after) < 2 {
			continue
		}

		fstype := after[0]
		if fstype != "fuse.rclone" && !strings.HasPrefix(fstype, "fuse.rclone") {
			continue
		}

		mounts = append(mounts, model.Mount{
			Remote:     unescapeMountField(after[1]),
			Mountpoint: unescapeMountField(before[4]),
			FSType:     fstype,
			Source:     model.SourceLocalFS,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

// unescapeMountField decodes the octal escapes the kernel uses for characters
// that would otherwise break the field separation. A mountpoint containing a
// space arrives as "My\040Drive".
func unescapeMountField(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+3 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		v := 0
		valid := true
		for _, c := range s[i+1 : i+4] {
			if c < '0' || c > '7' {
				valid = false
				break
			}
			v = v*8 + int(c-'0')
		}
		if !valid {
			b.WriteByte(s[i])
			continue
		}
		b.WriteByte(byte(v))
		i += 3
	}
	return b.String()
}
