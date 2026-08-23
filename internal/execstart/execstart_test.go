package execstart

import (
	"os"
	"path/filepath"
	"testing"
)

// writeScript puts a wrapper on disk and returns the ExecStart property systemd
// would record for a unit that runs it.
func writeScript(t *testing.T, body string) (path, execStart string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "wrapper")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("writing the wrapper: %v", err)
	}
	return path, "{ path=" + path + " ; argv[]=" + path + " ; ignore_errors=no }"
}

func TestDrivesRclone(t *testing.T) {
	_, wrapperExec := writeScript(t, "#!/bin/sh\nrclone sync a b\n")
	_, silentExec := writeScript(t, "#!/bin/sh\nrsync -a a b\n")

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "direct invocation",
			in:   `{ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone mount gdrive: /mnt ; ignore_errors=no }`,
			want: true,
		},
		{
			// The reference setup drives rclone from a wrapper script, so the
			// unit itself never names it. This is the case the whole
			// classification has to cope with, and the only way to settle it
			// short of a live process's cgroup is to read the script.
			name: "wrapper script",
			in:   wrapperExec,
			want: true,
		},
		{
			name: "a wrapper that drives something else",
			in:   silentExec,
			want: false,
		},
		{
			// The script is not there to be read, and inventing a verdict for
			// it would classify a unit on nothing at all.
			name: "wrapper script that is not on disk",
			in:   `{ path=/home/user/bin/jd-bisync ; argv[]=/home/user/bin/jd-bisync ; ignore_errors=no }`,
			want: false,
		},
		{
			name: "unrelated",
			in:   `{ path=/usr/bin/true ; argv[]=/usr/bin/true ; ignore_errors=no }`,
			want: false,
		},
		{name: "empty", in: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DrivesRclone(tt.in); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Only shell scripts, and only by their own account. Without the shebang check
// this would grind through any binary a unit happens to name and match on an
// incidental string -- "rclone" appears in plenty of them.
func TestOnlyShebangFilesAreRead(t *testing.T) {
	_, execStart := writeScript(t, "this file mentions rclone but starts no shell\n")
	if DrivesRclone(execStart) {
		t.Error("a file with no shebang was read as a wrapper script")
	}
}

func TestReferencedPaths(t *testing.T) {
	got := referencedPaths(`{ path=/home/user/bin/jd-bisync ; argv[]=/home/user/bin/jd-bisync ; ignore_errors=no }
{ path=/usr/bin/true ; argv[]=/usr/bin/true ; ignore_errors=no }`)
	want := []string{"/home/user/bin/jd-bisync", "/usr/bin/true"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}

// A unit that names rclone directly needs no script read at all.
func TestLogFileFromExecStartItself(t *testing.T) {
	execStart := `{ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone sync a b --log-file /var/log/rclone.log ; ignore_errors=no }`
	if got := LogFile(execStart, ""); got != "/var/log/rclone.log" {
		t.Errorf("got %q, want /var/log/rclone.log", got)
	}
}

// systemd records an argument vector without quoting, and a shell -c string is
// one argument that this splits on spaces. A path that ends it must not keep
// the quote that closed it: the tail would ask for a file of that name for ever.
func TestExecStartArgumentsAreUnquoted(t *testing.T) {
	execStart := `{ path=/bin/sh ; argv[]=/bin/sh -c "rclone sync a b --log-file /var/log/x.log" ; ignore_errors=no }`
	if got := LogFile(execStart, ""); got != "/var/log/x.log" {
		t.Errorf("got %q, want /var/log/x.log", got)
	}
}

// The whole of it, end to end: a unit whose ExecStart names nothing but a
// script, and a script that names the log only in terms of $HOME.
func TestLogFileFallsBackToTheWrapperScript(t *testing.T) {
	home := t.TempDir()
	_, execStart := writeScript(t,
		"#!/usr/bin/env bash\n"+
			`LOG_DIR="$HOME/.local/state/jd-backup"`+"\n"+
			`rclone bisync "$HOME/Documents" gdrive:Documents -v --log-file "$LOG_DIR/bisync.log"`+"\n")

	want := filepath.Join(home, ".local/state/jd-backup/bisync.log")
	if got := LogFile(execStart, home); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A system unit's $HOME belongs to root, or to whatever User= says. With no
	// honest answer for it the path is not resolved at all.
	if got := LogFile(execStart, ""); got != "" {
		t.Errorf("got %q with no home to resolve against", got)
	}
}

// A relative --log-file on the command line is not this package's to resolve:
// it is relative to the directory the unit was started in, which only the
// caller can know. Answering it here would name a different file with the same
// conviction.
func TestARelativeArgumentIsReturnedAsWritten(t *testing.T) {
	if got := LogFileFromArgs([]string{"rclone", "sync", "a", "b", "--log-file", "rclone.log"}); got != "rclone.log" {
		t.Errorf("got %q, want it back as written", got)
	}
	execStart := `{ path=/usr/bin/rclone ; argv[]=/usr/bin/rclone sync a b --log-file rclone.log ; ignore_errors=no }`
	if got := LogFile(execStart, "/home/user"); got != "" {
		t.Errorf("got %q, want nothing for a unit whose log file is relative", got)
	}
}

// The two shapes that actually occur. Both wrapper scripts on the host this was
// developed against write the same thing, and neither puts the path in the
// argument literally:
//
//	LOG_DIR="$HOME/.local/state/jd-backup"
//	rclone bisync … -v --log-file "$LOG_DIR/bisync.log"
//
// Refusing to resolve that leaves the feature finding nothing at all, so a
// literal assignment is followed -- which is reading two lines, not running
// them. Everything that would require actually executing the script is refused.
func TestLogFileFromScript(t *testing.T) {
	const home = "/home/user"

	cases := []struct {
		name   string
		script string
		home   string
		want   string
	}{
		{
			name: "through a variable, as both real wrappers write it",
			script: `#!/usr/bin/env bash
set -uo pipefail

LOG_DIR="$HOME/.local/state/jd-backup"
mkdir -p "$LOG_DIR"

rclone bisync "$HOME/Documents" gdrive:Documents \
    --resilient --recover \
    -v --log-file "$LOG_DIR/bisync.log"
`,
			home: home,
			want: "/home/user/.local/state/jd-backup/bisync.log",
		},
		{
			name:   "spelled out in full",
			script: "#!/bin/sh\nrclone sync a b --log-file /var/log/rclone/sync.log\n",
			home:   home,
			want:   "/var/log/rclone/sync.log",
		},
		{
			name:   "joined with an equals sign",
			script: `#!/bin/sh` + "\n" + `rclone sync a b --log-file="$HOME/rclone.log"` + "\n",
			home:   home,
			want:   "/home/user/rclone.log",
		},
		{
			name:   "braced variable",
			script: "#!/bin/sh\nD=/srv/logs\nrclone sync a b --log-file \"${D}/rclone.log\"\n",
			home:   home,
			want:   "/srv/logs/rclone.log",
		},
		{
			// A system unit's $HOME is root's, or whatever User= says. Guessing
			// it would name a file belonging to somebody else.
			name:   "no home to resolve against",
			script: "#!/bin/sh\nrclone sync a b --log-file \"$HOME/rclone.log\"\n",
			home:   "",
			want:   "",
		},
		{
			// Working out what this produces means running it.
			name:   "command substitution",
			script: "#!/bin/sh\nrclone sync a b --log-file \"$(date +%F).log\"\n",
			home:   home,
			want:   "",
		},
		{
			name:   "default-value expansion is still shell evaluation",
			script: "#!/bin/sh\nrclone sync a b --log-file \"${LOG_DIR:-/tmp}/x.log\"\n",
			home:   home,
			want:   "",
		},
		{
			name:   "a variable that is never assigned",
			script: "#!/bin/sh\nrclone sync a b --log-file \"$MYSTERY/rclone.log\"\n",
			home:   home,
			want:   "",
		},
		{
			// Relative to whatever directory systemd started the unit in.
			name:   "relative path",
			script: "#!/bin/sh\nrclone sync a b --log-file rclone.log\n",
			home:   home,
			want:   "",
		},
		{
			name:   "no log at all",
			script: "#!/bin/sh\nrclone sync a b -v\n",
			home:   home,
			want:   "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := logFileFromScript([]byte(c.script), c.home); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A truncated line must not take the program down. firstShellWord split on
// whitespace and took the first field, and a value that is nothing but a
// newline has no fields at all.
func TestLogFileFromScriptSurvivesATruncatedLine(t *testing.T) {
	for _, script := range []string{
		"#!/bin/sh\nrclone sync a b --log-file \n",
		"#!/bin/sh\nrclone sync a b --log-file=\n",
		"#!/bin/sh\nrclone sync a b --log-file \"unterminated\n",
		"#!/bin/sh\nrclone sync a b --log-file ",
	} {
		if got := logFileFromScript([]byte(script), "/home/user"); got != "" {
			t.Errorf("script %q gave %q, want nothing", script, got)
		}
	}
}

// The assignment that counts is the one in force where rclone is invoked. A
// script that reuses the name afterwards -- for a second job, or to tidy up --
// says nothing about the first.
func TestTheAssignmentBeforeTheCommandWins(t *testing.T) {
	script := "#!/bin/sh\n" +
		"LOG_DIR=/srv/first\n" +
		"rclone sync a b --log-file \"$LOG_DIR/rclone.log\"\n" +
		"LOG_DIR=/srv/second\n"

	if got := logFileFromScript([]byte(script), "/home/user"); got != "/srv/first/rclone.log" {
		t.Errorf("got %q, want /srv/first/rclone.log", got)
	}
}

// The parsing is textual, so it has to know what is not a command.
func TestLogFileFromScriptIgnoresComments(t *testing.T) {
	script := "#!/bin/sh\n" +
		"# old flag was --log-file /var/log/old.log\n" +
		"rclone sync a b --log-file /var/log/current.log\n"

	if got := logFileFromScript([]byte(script), "/home/user"); got != "/var/log/current.log" {
		t.Errorf("got %q, want the line that is not a comment", got)
	}
}

// A commented-out flag and nothing else means nothing was found, not that the
// stale path is current.
func TestAnEntirelyCommentedFlagFindsNothing(t *testing.T) {
	script := "#!/bin/sh\n# --log-file /var/log/old.log\nrclone sync a b\n"
	if got := logFileFromScript([]byte(script), "/home/user"); got != "" {
		t.Errorf("got %q from a comment", got)
	}
}

// Two invocations, two spellings. Whichever comes first is the one whose
// variables were resolved, so it must also be the one whose value is taken --
// otherwise the answer is a path neither command writes.
func TestTheFirstInvocationIsTheOneRead(t *testing.T) {
	script := "#!/bin/sh\n" +
		"LOG_DIR=/srv/first\n" +
		"rclone sync a b --log-file \"$LOG_DIR/one.log\"\n" +
		"LOG_DIR=/srv/second\n" +
		"rclone sync c d --log-file=\"$LOG_DIR/two.log\"\n"

	if got := logFileFromScript([]byte(script), "/home/user"); got != "/srv/first/one.log" {
		t.Errorf("got %q, want /srv/first/one.log", got)
	}
}

// export is how a great many scripts write an assignment.
func TestExportedAssignmentsAreRead(t *testing.T) {
	script := "#!/bin/sh\nexport LOG_DIR=/srv/logs\nrclone sync a b --log-file \"$LOG_DIR/x.log\"\n"
	if got := logFileFromScript([]byte(script), "/home/user"); got != "/srv/logs/x.log" {
		t.Errorf("got %q, want /srv/logs/x.log", got)
	}
}

// One assignment may be written in terms of another. Resolving $HOME costs a
// level of its own, so the budget has to allow for it.
func TestAssignmentsMayReferToEachOther(t *testing.T) {
	script := "#!/bin/sh\n" +
		"STATE=\"$HOME/.local/state\"\n" +
		"LOG_DIR=\"$STATE/jd-backup\"\n" +
		"rclone sync a b --log-file \"$LOG_DIR/x.log\"\n"

	want := "/home/user/.local/state/jd-backup/x.log"
	if got := logFileFromScript([]byte(script), "/home/user"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
