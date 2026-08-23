package collect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Seykhel/rclonetop/internal/model"
	"time"
)

// recentEpoch is a journal timestamp an hour in the past, in the microseconds
// journald reports.
//
// The fixtures here used a fixed epoch, and it worked until the day it did
// not: retention drops an entry older than a day, so the tests that assert an
// error is *kept* began failing twenty-four hours after the epoch was written
// down -- on the clock rather than on the code. Anything that asserts retention
// has to be fresh whenever the suite happens to run.
func recentEpoch() string {
	return strconv.FormatInt(time.Now().Add(-time.Hour).UnixMicro(), 10)
}

func TestParseShowBlocks(t *testing.T) {
	// Real output. systemctl accepts several units in one call and separates
	// the blocks with a blank line, which is what keeps this to one exec.
	out := `Id=jd-bisync.service
ActiveState=inactive
SubState=dead
FragmentPath=/home/user/.config/systemd/user/jd-bisync.service
ActiveEnterTimestamp=
InactiveEnterTimestamp=@1787433722
MainPID=0
Result=success
ExecMainCode=1
ExecMainStatus=0

Id=rclone-mount.service
ActiveState=active
SubState=running
ActiveEnterTimestamp=@1787383938
InactiveEnterTimestamp=
MainPID=2702
Result=success
ExecMainCode=0
ExecMainStatus=0
`
	blocks := parseShowBlocks(out)
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2", len(blocks))
	}
	if got := blocks[0]["Id"]; got != "jd-bisync.service" {
		t.Errorf("first Id = %q", got)
	}
	if got := blocks[1]["MainPID"]; got != "2702" {
		t.Errorf("second MainPID = %q", got)
	}
	// An empty value is a real answer -- "this never happened" -- not a
	// missing key.
	if got, ok := blocks[0]["ActiveEnterTimestamp"]; !ok || got != "" {
		t.Errorf("ActiveEnterTimestamp = %q, present %v; want empty and present", got, ok)
	}
}

func TestParseShowBlocksHandlesValuesWithEquals(t *testing.T) {
	// ExecStart embeds a whole command line, equals signs and all. Splitting
	// on every '=' would truncate it.
	out := `Id=x.service
ExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone sync a b --flag=value ; ignore_errors=no }
`
	blocks := parseShowBlocks(out)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if !strings.Contains(blocks[0]["ExecStart"], "--flag=value") {
		t.Errorf("ExecStart was truncated: %q", blocks[0]["ExecStart"])
	}
}

func TestParseUnixTimestamp(t *testing.T) {
	// --timestamp=unix prints an epoch prefixed with '@'. Asking for it avoids
	// parsing "Sat 2026-08-22 23:22:02 CEST", whose day and zone names depend
	// on the locale systemctl happens to run under.
	got, ok := parseUnixTimestamp("@1787433722")
	if !ok {
		t.Fatal("failed to parse a unix timestamp")
	}
	if want := time.Unix(1787433722, 0); !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}

	for _, in := range []string{"", "0", "@0", "n/a", "Sat 2026-08-22 23:22:02 CEST"} {
		if _, ok := parseUnixTimestamp(in); ok {
			t.Errorf("parseUnixTimestamp(%q) reported success", in)
		}
	}
}

func TestParseTimersJSON(t *testing.T) {
	// Real output. The epochs are microseconds, and a timer that has never run
	// reports zero rather than omitting the field.
	out := `[{"next":1787423660252121,"left":1787423660252121,"last":1787421678744038,"passed":34881300541,"unit":"jd-bisync.timer","activates":"jd-bisync.service"},
{"next":1787425474369987,"left":1787425474369987,"last":0,"passed":0,"unit":"jd-s3-backup.timer","activates":"jd-s3-backup.service"},
{"next":null,"last":1787384238147129,"unit":"grub-boot-success.timer","activates":"grub-boot-success.service"}]`

	timers, err := parseTimersJSON([]byte(out))
	if err != nil {
		t.Fatalf("parseTimersJSON: %v", err)
	}
	if len(timers) != 3 {
		t.Fatalf("got %d timers, want 3", len(timers))
	}

	first := timers["jd-bisync.timer"]
	if first.Activates != "jd-bisync.service" {
		t.Errorf("activates = %q", first.Activates)
	}
	if want := time.UnixMicro(1787423660252121); !first.Next.Equal(want) {
		t.Errorf("next = %s, want %s", first.Next, want)
	}
	if want := time.UnixMicro(1787421678744038); !first.Last.Equal(want) {
		t.Errorf("last = %s, want %s", first.Last, want)
	}

	// Zero means never, and must not become 1970.
	if got := timers["jd-s3-backup.timer"]; !got.Last.IsZero() {
		t.Errorf("a timer that never ran reports last = %s", got.Last)
	}
	// A timer with no next elapse reports null.
	if got := timers["grub-boot-success.timer"]; !got.Next.IsZero() {
		t.Errorf("a timer with no next elapse reports next = %s", got.Next)
	}
}

func TestParseJournalJSON(t *testing.T) {
	// Real output, trimmed. __REALTIME_TIMESTAMP is microseconds, and both it
	// and PRIORITY arrive as strings.
	out := `{"__CURSOR":"s=da0f;i=1059365","PRIORITY":"3","__REALTIME_TIMESTAMP":"1787417576870643","MESSAGE":"ERROR : vfs cache: failed to write to cache file: RootURL not set\n"}
{"__CURSOR":"s=da0f;i=1059366","PRIORITY":"6","__REALTIME_TIMESTAMP":"1787417576870700","MESSAGE":"NOTICE: all good"}
{"__CURSOR":"s=da0f;i=1059367","PRIORITY":"4","__REALTIME_TIMESTAMP":"1787417576870800","MESSAGE":"too many errors 11/10"}
`
	lines, cursor := parseJournalJSON([]byte(out), 4)

	// Only warning severity or worse: the informational line is dropped.
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}
	if lines[0].Priority != 3 {
		t.Errorf("priority = %d, want 3", lines[0].Priority)
	}
	// The trailing newline journald keeps would break the layout.
	if strings.HasSuffix(lines[0].Message, "\n") {
		t.Errorf("message keeps its trailing newline: %q", lines[0].Message)
	}
	if want := time.UnixMicro(1787417576870643); !lines[0].At.Equal(want) {
		t.Errorf("timestamp = %s, want %s", lines[0].At, want)
	}

	// The cursor must be the last entry seen, including the ones filtered out,
	// or every poll re-reads and re-filters the same tail forever.
	if cursor != "s=da0f;i=1059367" {
		t.Errorf("cursor = %q, want the last entry's", cursor)
	}
}

func TestParseJournalJSONIgnoresGarbage(t *testing.T) {
	// journalctl can emit a non-object line for a rotated or corrupt entry.
	// One bad line must not lose the rest of the batch.
	out := `{"PRIORITY":"3","__REALTIME_TIMESTAMP":"1787417576870643","MESSAGE":"first","__CURSOR":"a"}
not json at all
{"PRIORITY":"3","__REALTIME_TIMESTAMP":"bogus","MESSAGE":"bad timestamp","__CURSOR":"b"}
{"PRIORITY":"3","__REALTIME_TIMESTAMP":"1787417576870645","MESSAGE":"last","__CURSOR":"c"}
`
	lines, cursor := parseJournalJSON([]byte(out), 4)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if lines[0].Message != "first" || lines[2].Message != "last" {
		t.Errorf("lost entries around the garbage: %+v", lines)
	}
	if cursor != "c" {
		t.Errorf("cursor = %q, want %q", cursor, "c")
	}
}

func TestParseJournalMessageArray(t *testing.T) {
	// A message containing non-UTF-8 bytes is emitted as an array of byte
	// values rather than a string.
	out := `{"PRIORITY":"3","__REALTIME_TIMESTAMP":"1787417576870643","MESSAGE":[104,105],"__CURSOR":"a"}` + "\n"
	lines, _ := parseJournalJSON([]byte(out), 4)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Message != "hi" {
		t.Errorf("message = %q, want %q", lines[0].Message, "hi")
	}
}

func TestUnitFromCgroup(t *testing.T) {
	tests := []struct{ in, want string }{
		{
			// A user service, which is where rclone usually lives.
			in:   "1:net_cls:/\n0::/user.slice/user-1000.slice/user@1000.service/app.slice/rclone-mount.service\n",
			want: "rclone-mount.service",
		},
		{
			in:   "0::/system.slice/rclone-backup.service\n",
			want: "rclone-backup.service",
		},
		{
			// A scope, not a service: still the unit that owns the process.
			in:   "0::/user.slice/user-1000.slice/session-2.scope\n",
			want: "session-2.scope",
		},
		{in: "0::/\n", want: ""},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := unitFromCgroup(tt.in); got != tt.want {
			t.Errorf("unitFromCgroup(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// fakeRunner replays canned command output, so the collector is exercised
// without a systemd on the other end.
type fakeRunner struct {
	responses map[string][]byte
	errs      map[string]error
	calls     []string
}

func (f *fakeRunner) run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	for pattern, err := range f.errs {
		if strings.Contains(key, pattern) {
			return nil, err
		}
	}
	// Longest pattern first, and never in map order.
	//
	// One call can contain several patterns -- the properties query asks for
	// ActiveState and ExecStart together -- and ranging over the map would
	// answer with whichever Go happened to visit first, so the same test would
	// pass or fail from one run to the next. The most specific match is the
	// one that was meant.
	patterns := make([]string, 0, len(f.responses))
	for pattern := range f.responses {
		patterns = append(patterns, pattern)
	}
	sort.Slice(patterns, func(i, j int) bool { return len(patterns[i]) > len(patterns[j]) })
	for _, pattern := range patterns {
		if strings.Contains(key, pattern) {
			return f.responses[pattern], nil
		}
	}
	return nil, fmt.Errorf("unexpected command: %s", key)
}

func TestSystemdCollect(t *testing.T) {
	r := &fakeRunner{responses: map[string][]byte{
		"--version": []byte("systemd 257\n"),
		"list-units": []byte(`[{"unit":"rclone-mount.service","load":"loaded","active":"active","sub":"running"},
{"unit":"jd-bisync.service","load":"loaded","active":"inactive","sub":"dead"},
{"unit":"unrelated.service","load":"loaded","active":"active","sub":"running"}]`),
		"list-timers": []byte(`[{"next":1787423660252121,"last":1787421678744038,"unit":"jd-bisync.timer","activates":"jd-bisync.service"},
{"next":1787425474369987,"last":0,"unit":"unrelated.timer","activates":"unrelated.service"}]`),
		"-p ExecStart": []byte(`Id=rclone-mount.service
ExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone mount gdrive: /mnt ; ignore_errors=no }

Id=jd-bisync.service
ExecStart={ path=/nonexistent/jd-bisync ; argv[]=/nonexistent/jd-bisync ; ignore_errors=no }

Id=unrelated.service
ExecStart={ path=/usr/bin/true ; argv[]=/usr/bin/true ; ignore_errors=no }
`),
		"-p ActiveState": []byte(`Id=rclone-mount.service
ActiveState=active
SubState=running
Result=success
ExecMainCode=0
ExecMainStatus=0
ActiveEnterTimestamp=@1787383938
InactiveEnterTimestamp=
MainPID=2702

Id=jd-bisync.timer
ActiveState=active
SubState=waiting
Result=success
ExecMainCode=0
ExecMainStatus=0
ActiveEnterTimestamp=@1787380000
InactiveEnterTimestamp=
MainPID=0
`),
		"journalctl": []byte(`{"PRIORITY":"3","__REALTIME_TIMESTAMP":"` + recentEpoch() + `","MESSAGE":"vfs cache: RootURL not set","__CURSOR":"c1"}` + "\n"),
	}}

	s := newSystemdWith(r.run, []string{"user"})
	// The bisync unit is only discoverable through its cgroup, which is how a
	// wrapper script gets attributed to its unit.
	s.noteUnit("user", "jd-bisync.service")

	snap, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	byName := map[string]bool{}
	for _, u := range snap.Units {
		byName[u.Name] = true
	}
	if !byName["rclone-mount.service"] {
		t.Error("the unit whose ExecStart names rclone was not reported")
	}
	if !byName["jd-bisync.timer"] {
		t.Error("the timer of a known rclone unit was not reported")
	}
	if byName["unrelated.service"] || byName["unrelated.timer"] {
		t.Error("an unrelated unit was reported")
	}

	for _, u := range snap.Units {
		if u.Name != "jd-bisync.timer" {
			continue
		}
		if want := time.UnixMicro(1787423660252121); !u.NextElapse.Equal(want) {
			t.Errorf("next elapse = %s, want %s", u.NextElapse, want)
		}
		if u.Triggers != "jd-bisync.service" {
			t.Errorf("triggers = %q", u.Triggers)
		}
	}
}

func TestSystemdUnavailableWhenSystemctlFails(t *testing.T) {
	// A host without systemd must degrade to hiding the section, not to an
	// error banner on every tick.
	r := &fakeRunner{errs: map[string]error{"systemctl": errors.New("not found")}}
	s := newSystemdWith(r.run, []string{"user"})

	if s.Available() {
		t.Error("Available reported true with no systemd")
	}
	snap, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect should not fail: %v", err)
	}
	if snap.Units == nil {
		t.Error("Units is nil, so a stale list would linger on screen")
	}
	if len(snap.Units) != 0 {
		t.Errorf("got %d units, want none", len(snap.Units))
	}
}

func TestSystemdJournalCursorAdvances(t *testing.T) {
	// The tail must resume from the cursor, or every poll re-reads the whole
	// backlog and the same errors are shown forever.
	r := &fakeRunner{responses: map[string][]byte{
		"--version":      []byte("systemd 257\n"),
		"list-units":     []byte(`[{"unit":"rclone-mount.service","active":"active","sub":"running"}]`),
		"list-timers":    []byte(`[]`),
		"-p ExecStart":   []byte("Id=rclone-mount.service\nExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone mount a: /b }\n"),
		"-p ActiveState": []byte("Id=rclone-mount.service\nActiveState=active\nSubState=running\nResult=success\n"),
		"journalctl":     []byte(`{"PRIORITY":"3","__REALTIME_TIMESTAMP":"` + recentEpoch() + `","MESSAGE":"boom","__CURSOR":"cursor-1"}` + "\n"),
	}}
	s := newSystemdWith(r.run, []string{"user"})

	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("second Collect: %v", err)
	}

	var resumed bool
	for _, call := range r.calls {
		if strings.Contains(call, "journalctl") && strings.Contains(call, "--after-cursor cursor-1") {
			resumed = true
		}
	}
	if !resumed {
		t.Errorf("the second poll did not resume from the cursor: %v", r.calls)
	}
}

// TestSystemdKeepsRecentErrors covers the interaction between an incremental
// tail and a repeating poll. The journal is read from a cursor, so each poll
// returns only what is new; replacing the unit's errors with that would make a
// problem flash for a single frame and then vanish, leaving a unit that failed
// a minute ago looking clean.
func TestSystemdKeepsRecentErrors(t *testing.T) {
	r := &fakeRunner{responses: map[string][]byte{
		"--version":      []byte("systemd 257\n"),
		"list-units":     []byte(`[{"unit":"rclone-mount.service","active":"active","sub":"running"}]`),
		"list-timers":    []byte(`[]`),
		"-p ExecStart":   []byte("Id=rclone-mount.service\nExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone mount a: /b }\n"),
		"-p ActiveState": []byte("Id=rclone-mount.service\nActiveState=active\nSubState=running\nResult=success\n"),
		"journalctl":     []byte(`{"PRIORITY":"3","__REALTIME_TIMESTAMP":"` + recentEpoch() + `","MESSAGE":"boom","__CURSOR":"c1"}` + "\n"),
	}}
	s := newSystemdWith(r.run, []string{"user"})

	first, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(first.Units) != 1 || len(first.Units[0].Errors) != 1 {
		t.Fatalf("first poll reported %d units with %d errors", len(first.Units), len(first.Units[0].Errors))
	}

	// Nothing new since the cursor.
	r.responses["journalctl"] = []byte("")
	second, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(second.Units[0].Errors) != 1 {
		t.Errorf("the error vanished on the next poll: %+v", second.Units[0].Errors)
	}
	if second.Units[0].Errors[0].Message != "boom" {
		t.Errorf("wrong error retained: %+v", second.Units[0].Errors)
	}
}

func TestSystemdBoundsRetainedErrors(t *testing.T) {
	s := newSystemdWith(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("unused")
	}, []string{"user"})

	for i := 0; i < maxUnitErrors*3; i++ {
		s.remember("user/x.service", []model.LogLine{{Priority: 3, Message: fmt.Sprint(i)}})
	}
	got := s.recent["user/x.service"]
	if len(got) != maxUnitErrors {
		t.Fatalf("retained %d errors, want %d", len(got), maxUnitErrors)
	}
	// The newest must survive, not the oldest.
	if got[len(got)-1].Message != fmt.Sprint(maxUnitErrors*3-1) {
		t.Errorf("kept the wrong end: %+v", got)
	}
}

// TestConcurrentNoteAndCollect pins the goroutine boundary this collector sits
// across. The process collector calls NoteProcesses on its own one-second tick
// while this one classifies on its five-second tick, both touching the same
// maps. Go aborts the whole program on a concurrent map access, so this failing
// under -race means a crashed TUI in practice.
func TestConcurrentNoteAndCollect(t *testing.T) {
	r := &fakeRunner{responses: map[string][]byte{
		"--version":      []byte("systemd 257\n"),
		"list-units":     []byte(`[{"unit":"a.service","active":"active","sub":"running"}]`),
		"list-timers":    []byte(`[]`),
		"-p ExecStart":   []byte("Id=a.service\nExecStart={ path=/usr/bin/true ; argv[]=/usr/bin/true }\n"),
		"-p ActiveState": []byte("Id=a.service\nActiveState=active\n"),
		"journalctl":     []byte(""),
	}}
	s := newSystemdWith(r.run, []string{"user"})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			s.noteUnit("user", "b.service")
			s.remember("user/a.service", []model.LogLine{{Priority: 3, Message: "x"}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := s.Collect(context.Background()); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	wg.Wait()
}

// TestAllScopesFailingIsReported covers a silent-failure path. If every scope
// stops answering -- lost privileges, a dead system bus -- the unit list goes
// empty, which on screen is indistinguishable from genuinely having no rclone
// units. The collector interface exists so the UI can tell those apart.
func TestAllScopesFailingIsReported(t *testing.T) {
	r := &fakeRunner{
		responses: map[string][]byte{"--version": []byte("systemd 257\n")},
		errs:      map[string]error{"list-units": errors.New("Failed to connect to bus")},
	}
	s := newSystemdWith(r.run, []string{"user", "system"})

	snap, err := s.Collect(context.Background())
	if err == nil {
		t.Fatal("every scope failed and Collect reported success")
	}
	if !strings.Contains(err.Error(), "bus") {
		t.Errorf("the reason was lost: %v", err)
	}
	if snap.Units == nil {
		t.Error("Units is nil, so a stale list would linger on screen")
	}
}

// TestOneScopeFailingIsNotFatal is the converse: a user session can exist
// without a reachable system bus, and what the other scope found is still worth
// showing.
func TestOneScopeFailingIsNotFatal(t *testing.T) {
	calls := 0
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "--version"):
			return []byte("systemd 257\n"), nil
		case strings.Contains(joined, "--system"):
			calls++
			return nil, errors.New("Failed to connect to bus")
		case strings.Contains(joined, "list-units"):
			return []byte(`[{"unit":"rclone-mount.service","active":"active","sub":"running"}]`), nil
		case strings.Contains(joined, "list-timers"):
			return []byte(`[]`), nil
		case strings.Contains(joined, "-p ExecStart"):
			return []byte("Id=rclone-mount.service\nExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone mount a: /b }\n"), nil
		case strings.Contains(joined, "-p ActiveState"):
			return []byte("Id=rclone-mount.service\nActiveState=active\nSubState=running\nResult=success\n"), nil
		default:
			return []byte(""), nil
		}
	}
	s := newSystemdWith(run, []string{"user", "system"})

	snap, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("one failing scope should not fail the collection: %v", err)
	}
	if len(snap.Units) != 1 {
		t.Errorf("got %d units, want the one the healthy scope found", len(snap.Units))
	}
	if calls == 0 {
		t.Error("the failing scope was never attempted")
	}
}

// TestShowSeparatesUnitNamesFromFlags covers argument injection. A unit name
// may begin with a dash -- "-.mount" exists on every system -- and without the
// separator systemctl reads it as a flag: "show -p Id --version" prints the
// systemd version, and -H or -M would redirect the call to another host.
func TestShowSeparatesUnitNamesFromFlags(t *testing.T) {
	r := &fakeRunner{responses: map[string][]byte{"show": []byte("Id=x.service\n")}}
	s := newSystemdWith(r.run, []string{"user"})

	if _, err := s.show(context.Background(), "user", []string{"--version.service"}, "Id"); err != nil {
		t.Fatalf("show: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("got %d calls", len(r.calls))
	}
	call := r.calls[0]
	sep := strings.Index(call, " -- ")
	if sep < 0 {
		t.Fatalf("no -- separator before the unit names: %s", call)
	}
	if strings.Index(call, "--version.service") < sep {
		t.Errorf("a unit name precedes the separator: %s", call)
	}
}

// TestCgroupAttributionSurvivesClassification is a regression test.
//
// Classification takes over a second on a host with many units, and in that
// window the process collector can attribute a unit from a live rclone's
// cgroup. Writing the classifier's verdict unconditionally overwrote that
// with a negative, and because negatives are cached and never revisited, the
// unit disappeared until rclonetop was restarted -- losing exactly the case
// the cgroup signal exists for.
func TestCgroupAttributionSurvivesClassification(t *testing.T) {
	s := newSystemdWith(func(context.Context, string, ...string) ([]byte, error) {
		// The wrapper never names rclone, so the classifier says no.
		return []byte("Id=jd-bisync.service\nExecStart={ path=/nonexistent/wrapper ; argv[]=/nonexistent/wrapper }\n"), nil
	}, []string{"user"})

	s.noteUnit("user", "jd-bisync.service")
	wanted := s.classify(context.Background(), "user", []string{"jd-bisync.service"})

	if !wanted["jd-bisync.service"] {
		t.Error("the cgroup attribution was overwritten by the classifier")
	}
	if !s.relevant["user/jd-bisync.service"] {
		t.Error("the negative verdict was cached over the positive one")
	}
}

// TestErrorsAreForgottenAfterASuccessfulRun covers a job that failed and has
// since been running cleanly. A scheduled bisync that broke at six and has
// succeeded every half hour since is not a job with a problem, and keeping the
// old entry on screen makes it look like one.
func TestErrorsAreForgottenAfterASuccessfulRun(t *testing.T) {
	s := newSystemdWith(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("unused")
	}, []string{"user"})

	failedAt := time.Now().Add(-6 * time.Hour)
	s.remember("user/jd-bisync.service", []model.LogLine{
		{At: failedAt, Priority: 3, Message: "Failed with result 'signal'"},
	})

	u := model.Unit{
		Name: "jd-bisync.service", Result: "success",
		ActiveState: "inactive", SubState: "dead",
		// A run that finished after the failure.
		InactiveEnter: failedAt.Add(time.Hour),
		Errors:        []model.LogLine{{At: failedAt, Priority: 3, Message: "Failed with result 'signal'"}},
	}
	if got := s.forgetResolved("user", u); got != nil {
		t.Errorf("a superseded error survived: %+v", got)
	}
	if _, ok := s.recent["user/jd-bisync.service"]; ok {
		t.Error("the retained tail was not cleared")
	}
}

func TestErrorsSurviveWhenNothingHasSucceededSince(t *testing.T) {
	s := newSystemdWith(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("unused")
	}, []string{"user"})
	failedAt := time.Now().Add(-time.Hour)
	errs := []model.LogLine{{At: failedAt, Priority: 3, Message: "boom"}}

	// A long-running service never "succeeds", so its errors must age out
	// rather than be cleared: InactiveEnter stays zero for as long as it runs.
	mount := model.Unit{
		Name: "rclone-mount.service", Result: "success",
		ActiveState: "active", SubState: "running", Errors: errs,
	}
	if got := s.forgetResolved("user", mount); len(got) != 1 {
		t.Errorf("a running mount lost its errors: %+v", got)
	}

	// A job whose last run ended before the error is not evidence of anything.
	stale := model.Unit{
		Name: "job.service", Result: "success",
		ActiveState: "inactive", SubState: "dead",
		InactiveEnter: failedAt.Add(-time.Hour),
		Errors:        errs,
	}
	if got := s.forgetResolved("user", stale); len(got) != 1 {
		t.Errorf("an error newer than the last run was dropped: %+v", got)
	}

	// And a unit that is still failing keeps them regardless.
	failing := model.Unit{
		Name: "job.service", Result: "exit-code", ActiveState: "failed",
		InactiveEnter: time.Now(), Errors: errs,
	}
	if got := s.forgetResolved("user", failing); len(got) != 1 {
		t.Errorf("a failing unit lost its errors: %+v", got)
	}
}

// The unit is what knows where a scheduled job writes, and it knows it between
// runs -- which is the whole point, because between runs is when there is no
// process to ask.
func TestLogFilesAreDiscoveredFromUnits(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "jd-bisync")
	if err := os.WriteFile(wrapper, []byte(
		"#!/usr/bin/env bash\n"+
			`LOG_DIR="$HOME/.local/state/jd-backup"`+"\n"+
			`rclone bisync "$HOME/Documents" gdrive:Documents -v --log-file "$LOG_DIR/bisync.log"`+"\n",
	), 0o755); err != nil {
		t.Fatalf("writing the wrapper: %v", err)
	}

	r := &fakeRunner{responses: map[string][]byte{
		"--version":   []byte("systemd 257\n"),
		"list-units":  []byte(`[{"unit":"jd-bisync.service","load":"loaded","active":"inactive","sub":"dead"}]`),
		"list-timers": []byte(`[]`),
		"-p ExecStart": []byte("Id=jd-bisync.service\n" +
			"ExecStart={ path=" + wrapper + " ; argv[]=" + wrapper + " ; ignore_errors=no }\n"),
		"-p ActiveState": []byte("Id=jd-bisync.service\n" +
			"ActiveState=inactive\nSubState=dead\nResult=success\n" +
			"ExecMainCode=1\nExecMainStatus=0\nMainPID=0\n" +
			"ExecStart={ path=" + wrapper + " ; argv[]=" + wrapper + " ; ignore_errors=no }\n"),
		"journalctl": []byte(""),
	}}

	s := newSystemdWith(r.run, []string{"user"})
	// The home the script's $HOME stands for. In a user unit that is the user
	// rclonetop is running as; the fixture stands in for it.
	s.home = dir

	var got []string
	s.OnLogFiles(func(paths []string) { got = paths })

	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	want := filepath.Join(dir, ".local/state/jd-backup/bisync.log")
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %v, want [%s]", got, want)
	}
}

// A unit that goes away must not leave its log path behind: the next unit to
// wear the name would be followed to the old one's file.
func TestTheLogFileCacheIsPruned(t *testing.T) {
	s := newSystemdWith(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("unused")
	}, []string{"user"})
	s.logFiles["user/gone.service"] = "/var/log/gone.log"
	s.logFiles["user/stays.service"] = "/var/log/stays.log"

	s.prune("user", []string{"stays.service"})

	if _, ok := s.logFiles["user/gone.service"]; ok {
		t.Error("a unit that is no longer listed kept its log path")
	}
	if _, ok := s.logFiles["user/stays.service"]; !ok {
		t.Error("a unit still listed lost its log path")
	}
}

// One scope failing must not retract the other's findings, and must not retract
// its own from the last time it answered. The log collector replaces its whole
// set on every call, so a path missing for one tick is a tail dropped -- and an
// idle job's last line can easily be more than an hour old, which is when a tail
// with no process is forgotten.
func TestAFailingScopeDoesNotRetractWhatItFound(t *testing.T) {
	// Scoped patterns, so each scope answers for its own units. The show
	// pattern has to be longer than "-p ActiveState" to win the longest match.
	unitBlock := func(name, log string) []byte {
		return []byte("Id=" + name + "\n" +
			"ActiveState=inactive\nSubState=dead\nResult=success\n" +
			"ExecStart={ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone sync a b --log-file " + log + " ; ignore_errors=no }\n")
	}
	r := &fakeRunner{responses: map[string][]byte{
		"--version":                []byte("systemd 257\n"),
		"--user list-units":        []byte(`[{"unit":"rclone-user.service","active":"inactive","sub":"dead"}]`),
		"--system list-units":      []byte(`[{"unit":"rclone-system.service","active":"inactive","sub":"dead"}]`),
		"--user list-timers":       []byte(`[]`),
		"--system list-timers":     []byte(`[]`),
		"--user show --no-pager":   unitBlock("rclone-user.service", "/var/log/user.log"),
		"--system show --no-pager": unitBlock("rclone-system.service", "/var/log/system.log"),
		"journalctl":               []byte(""),
	}}
	s := newSystemdWith(r.run, []string{"user", "system"})

	var got []string
	s.OnLogFiles(func(paths []string) { got = paths })

	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("first pass found %v, want both scopes' logs", got)
	}

	// The system scope stops answering. What it told us last time is still
	// true: the unit did not go away, the bus did.
	r.errs = map[string]error{"--system list-units": errors.New("no system bus")}
	if _, err := s.Collect(context.Background()); err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("a scope failing retracted its log: %v", got)
	}
}
