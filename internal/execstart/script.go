package execstart

import (
	"path"
	"regexp"
	"strings"
)

// shellAssignment matches a plain assignment at the start of a line:
// VAR=value, with or without quotes around the value, and with or without the
// export a great many scripts write in front of it.
var shellAssignment = regexp.MustCompile(
	`(?m)^\s*(?:export\s+|declare\s+(?:-\w+\s+)*|local\s+)?([A-Za-z_][A-Za-z0-9_]*)=("[^"]*"|'[^']*'|\S+)`)

// shellVariable matches a reference, braced or bare.
var shellVariable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// maxExpansionDepth bounds how far one assignment may refer to another, so a
// script whose variables refer to each other in a circle cannot spin here.
//
// A level is spent on each substitution, including the one that resolves $HOME
// at the end of a chain, so four allows the shapes that occur -- LOG_DIR built
// from STATE built from $HOME -- with one to spare.
const maxExpansionDepth = 4

// logFileFromScript finds the log file a wrapper script tells rclone to write.
//
// A unit that runs rclone from a script is the common arrangement, and the
// script rarely spells the path out: both wrappers this was developed against
// write the same thing,
//
//	LOG_DIR="$HOME/.local/state/jd-backup"
//	rclone bisync … -v --log-file "$LOG_DIR/bisync.log"
//
// so refusing to follow a variable would leave this finding nothing at all. A
// plain assignment is therefore substituted, and $HOME with it when the unit
// runs as the user rclonetop is running as.
//
// That is reading two lines, not running them, and the line between the two is
// drawn deliberately: command substitution, default-value expansion, a variable
// that is never assigned, or a path that does not come out absolute all mean
// this returns nothing. Working out what those produce means executing the
// script, which is not something a monitor may do.
//
// home is the directory $HOME stands for, and is empty when there is no honest
// answer -- a system unit's home belongs to root or to whatever User= says.
func logFileFromScript(body []byte, home string) string {
	// A commented-out flag is not a command. Left in, an old path someone
	// struck out months ago is tailed and presented as the unit's current
	// state.
	script := stripComments(string(body))

	raw, at := logFileArgument(script)
	if raw == "" {
		return ""
	}

	// Only the assignments above that command, because those are the ones in
	// force where rclone is invoked. A script that reuses the name afterwards
	// -- to tidy up, or for a second job -- says nothing about this one, and
	// the value and its variables have to be taken from the same invocation or
	// the answer is a path neither of them writes.
	vars := map[string]string{}
	for _, m := range shellAssignment.FindAllStringSubmatch(script[:at], -1) {
		vars[m[1]] = unquote(m[2])
	}
	if home != "" {
		vars["HOME"] = home
	}

	value, ok := expandShell(unquote(raw), vars, maxExpansionDepth)
	if !ok || !path.IsAbs(value) {
		// A relative path is relative to whatever directory systemd started
		// the unit in, which is not this one.
		return ""
	}
	return value
}

// logFileArgument finds the --log-file argument in shell source, in either
// spelling, and returns it exactly as written along with where it was found.
//
// The two spellings compete by position rather than by preference: a script
// with two invocations must not have the value of one read against the
// variables of the other.
func logFileArgument(script string) (value string, at int) {
	best := -1
	var rest string
	for _, form := range []string{"--log-file=", "--log-file "} {
		i := strings.Index(script, form)
		if i < 0 || (best >= 0 && i > best) {
			continue
		}
		best = i
		rest = strings.TrimLeft(script[i+len(form):], " \t")
	}
	if best < 0 {
		return "", 0
	}
	return firstShellWord(rest), best
}

// stripComments blanks out the lines a shell would never run.
//
// Whole-line comments only. A '#' partway through a line starts one too, but
// cutting there would also cut a path that legitimately contains one, and the
// case that occurs is a flag struck out by putting a hash in front of it.
func stripComments(script string) string {
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// firstShellWord takes one argument off the front of a command line, honouring
// a single level of quoting.
func firstShellWord(s string) string {
	if s == "" {
		return ""
	}
	if q := s[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(s[1:], q); end >= 0 {
			return s[:end+2]
		}
		return ""
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '&' || r == '|'
	})
	if len(fields) == 0 {
		// The flag was the last thing on the line, with nothing after it. Not
		// a path, and indexing the empty result would take the program down.
		return ""
	}
	return fields[0]
}

// unquote strips one layer of shell quoting.
func unquote(s string) string {
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// expandShell substitutes the variables it can and refuses everything else.
func expandShell(s string, vars map[string]string, depth int) (string, bool) {
	// Anything whose value depends on running something is refused outright:
	// command substitution, and the ${VAR:-default} family, which is a
	// conditional in disguise.
	if strings.Contains(s, "$(") || strings.Contains(s, "`") || strings.Contains(s, ":-") {
		return "", false
	}
	if depth <= 0 {
		return "", false
	}

	ok := true
	out := shellVariable.ReplaceAllStringFunc(s, func(ref string) string {
		name := strings.Trim(ref, "${}")
		value, known := vars[name]
		if !known {
			ok = false
			return ""
		}
		// An assignment may itself be written in terms of another.
		nested, good := expandShell(value, vars, depth-1)
		if !good {
			ok = false
			return ""
		}
		return nested
	})
	return out, ok
}
