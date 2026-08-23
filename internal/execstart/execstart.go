// Package execstart reads what a systemd unit actually runs.
//
// A unit rarely says "rclone" itself. The common arrangement is a wrapper
// script named by ExecStart, with rclone somewhere inside it and the log file
// spelled out in terms of shell variables -- which means the two questions the
// systemd collector has to answer, "does this unit drive rclone" and "where
// does its rclone write", are both a matter of reading a command line, then
// possibly a script, rather than of asking systemd anything.
//
// That reading is the whole of this package. It is deliberately narrow about
// what it will touch: regular files under a size limit that begin with a
// shebang, opened non-blocking, and never executed. Working out what a script
// would produce means running it, which is not something a monitor may do; when
// the answer is not legible from the text it is not guessed at.
package execstart

import (
	"bytes"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
)

// scanLimit bounds how much of a wrapper script is read.
const scanLimit = 256 << 10

// execStartField matches the "path=" entries systemd writes in ExecStart.
var execStartField = regexp.MustCompile(`path=([^\s;]+)`)

// execStartArgv matches the argument vector systemd records alongside it.
var execStartArgv = regexp.MustCompile(`argv\[\]=([^;]*)`)

// DrivesRclone reports whether the command a unit records runs rclone, directly
// or through the wrapper script it names.
//
// The script is only read when the command line itself does not settle it,
// because reading costs a syscall and the answer is cached by the caller.
func DrivesRclone(execStart string) bool {
	if strings.Contains(execStart, "rclone") {
		return true
	}
	for _, p := range referencedPaths(execStart) {
		if body := readScript(p); body != nil && bytes.Contains(body, []byte("rclone")) {
			return true
		}
	}
	return false
}

// LogFile is the file this unit's rclone writes to, or "" when that is not
// legible.
//
// Two places, in order of certainty: the recorded command line, when the unit
// runs rclone itself, and otherwise the wrapper script it runs instead.
//
// home is what $HOME stands for in that script, and must be empty when there is
// no honest answer -- a system unit's home belongs to root, or to whatever
// User= says, and guessing it would name somebody else's file.
func LogFile(execStart, home string) string {
	if found := LogFileFromArgs(argv(execStart)); path.IsAbs(found) {
		return found
	}
	for _, p := range referencedPaths(execStart) {
		if body := readScript(p); body != nil {
			if found := logFileFromScript(body, home); found != "" {
				return found
			}
		}
	}
	return ""
}

// LogFileFromArgs finds the log file in an argument vector, in either of the
// two spellings Go's flag package and rclone's both accept.
//
// It is exported because the same reading applies to the vector of a live
// process, which the log collector takes from /proc rather than from systemd.
// The result is returned as written: a relative path is only resolvable against
// the working directory of whatever wrote it, which is not this package's to
// know.
func LogFileFromArgs(args []string) string {
	for i, a := range args {
		switch {
		case a == "--log-file" && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, "--log-file="):
			return strings.TrimPrefix(a, "--log-file=")
		}
	}
	return ""
}

// argv splits the argument vector systemd records, so a unit that invokes
// rclone directly can be read the same way a live process is.
func argv(execStart string) []string {
	m := execStartArgv.FindStringSubmatch(execStart)
	if m == nil {
		return nil
	}
	args := strings.Fields(m[1])
	for i, a := range args {
		// systemd records the vector without quoting it back, so a shell -c
		// string arrives as several fields with the quotes that opened and
		// closed it still glued to the first and last. A path carrying one
		// would be asked for by that name for ever.
		args[i] = strings.Trim(a, `"'`)
	}
	return args
}

// referencedPaths lists the files an ExecStart property refers to.
//
// The path= field alone is not enough: "ExecStart=/bin/sh /usr/local/bin/sync"
// records path=/bin/sh, and the script that actually drives rclone appears only
// in argv. Absolute arguments are therefore returned too.
func referencedPaths(execStart string) []string {
	var paths []string
	seen := make(map[string]bool)
	add := func(p string) {
		if p != "" && path.IsAbs(p) && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	for _, m := range execStartField.FindAllStringSubmatch(execStart, -1) {
		add(m[1])
	}
	for _, m := range execStartArgv.FindAllStringSubmatch(execStart, -1) {
		for _, arg := range strings.Fields(m[1]) {
			add(arg)
		}
	}
	return paths
}

// readScript returns a wrapper script's contents, or nil when the path is not
// one this package will read.
//
// Deliberately narrow: regular files under a size limit that begin with a
// shebang, so it never grinds through a binary a unit happens to name.
func readScript(p string) []byte {
	if p == "" || !path.IsAbs(p) {
		return nil
	}

	// Opened with O_NONBLOCK and inspected through the descriptor, never by
	// path. Stat-then-open would leave a window for the path to be swapped,
	// and opening a FIFO for reading blocks until a writer appears -- which
	// would wedge the calling collector's goroutine indefinitely. O_NONBLOCK
	// makes that open return instead, and the fstat below rejects it anyway.
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > scanLimit {
		return nil
	}

	body := make([]byte, scanLimit)
	n, err := io.ReadFull(f, body)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil
	}
	body = body[:n]

	// Only shell scripts. Without the shebang check this would grind through
	// any binary, and match on an incidental string.
	if !bytes.HasPrefix(body, []byte("#!")) {
		return nil
	}
	return body
}
