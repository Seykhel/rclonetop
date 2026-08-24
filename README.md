# rclonetop

A terminal monitor for [rclone](https://rclone.org), in the style of
[btop++](https://github.com/aristocratos/btop).

It shows what rclone is doing on this host — running mounts and transfers,
throughput, synchronised files, space used — and it works whether or not rclone
was started in any particular way.

```
rclonetop 0.1.0 workstation ─────────────────────────────────── 21:15:40

MOUNT  gdrive: → ~/My Drive
  pid 2702 · up 11h43m · rss 75 MiB · thr 15
  ↓ 1.4 MiB/s   ⠀⠀⠀⠀⠀⢀⣠⣴⣾  ↑ 0 B/s       ⠀⠀⠀⠀⠀⠀⠀⠀⠀  ·  rd 130 MiB · wr 132 MiB

BISYNC ~/Documents → gdrive:Documents
  pid 193345 · up 4m36s · rss 85 MiB · thr 13
  ↓ 82 KiB/s    ⠀⠀⠀⢠⣾⣷⣶⣤⣀  ↑ 12 KiB/s    ⠀⠀⠀⠀⠀⢀⣠⣴⣾  ·  rd 3.5 MiB · wr 2.9 MiB
  58% · 2.9 GiB / 4.9 GiB · 1158/4667 files · ETA 2m51s

SYNC   ~/Documents ⇄ gdrive:Documents
  4710 files 5.0 GiB  ⇄  4710 files 5.0 GiB
  in sync · listed 21m46s ago · last failure 4h51m ago

UNIT   jd-bisync  idle
  last 2m0s ago · next in 28m0s
  ! 5h1m ago  jd-bisync.service: Main process exited, code=killed, status=15…
    and 3 more recent
UNIT   rclone-mount  running
  running for 11h43m

CACHE  vfs 47 MiB (326 files) · vfsMeta 1.6 MiB (404 files) · scanned 2s ago

────────────────────────────────────────────────────────────────────────────────
sources bisync · localfs · proc · systemd                   700ms  q quit
```

## Why another rclone TUI

The existing ones talk to rclone's [remote control
API](https://rclone.org/rc/) and nothing else. That is a reasonable choice, and
it means they show an empty screen on most hosts.

Very few rclone installations actually run the rc API. rclone is typically
driven by cron or a systemd timer, or left running as a FUSE mount — none of
which expose anything to connect to. You cannot see your nightly backup in a
monitor that can only speak rc, because there is nothing listening while it
runs.

rclonetop reads from whatever is available instead:

| Source | What it gives | Status |
|---|---|---|
| `/proc` | running processes, their operands, memory, and throughput from the kernel's byte counters | done |
| btop themes | the colour scheme, read from the themes already installed for btop | done |
| bisync listings | files and bytes on each side, drift, last run and last failure | done |
| local filesystem | `fuse.rclone` mounts, and the disk the caches occupy | done |
| systemd / journald | unit state, how the last run ended, next timer elapse, errors | done |
| rclone logs | job progress from `--log-file`, plain or `--use-json-log`, and the real paths of a bisync pair | done |
| rc API | exact statistics, when a daemon does expose it | planned |

Sources are independent. Whatever is unavailable is hidden rather than shown as
zero: a zero and an unreadable counter mean very different things to someone
checking whether their backup ran.

Progress is a separate question from throughput, and only rclone can answer it.
The kernel's byte counters say what has gone past; the log says how much there
was to begin with, so a transfer that is nearly done can be told from one that
has barely started. rclonetop follows the log file a running rclone was started
with — from its `--log-file` argument, whether it writes plain text or
`--use-json-log`, preferring the latter because its byte counts have not been
rounded on the way out.

The same log is the only place a bisync pair's paths appear in full: the
listings on disk name a session by mangling both paths into one filename, which
cannot be undone. What the log says is matched back to the listing by mangling
it the same way, so the paths shown are the ones that were typed rather than a
guess at them.

Throughput is graphed from the samples collected while rclonetop runs, in
braille, eighth-blocks or plain ASCII. Each graph scales to the busiest moment
in its own window, and both directions of a transfer share that scale so they
stay comparable — but any traffic at all leaves a mark, however small next to
the peak.

## Read-only

rclonetop never writes anything. It does not change rclone's configuration,
start or stop transfers, touch remotes, or control systemd units.

It does read more of the host than a monitor might be expected to, so here is
the whole of it:

- **Subprocesses.** `systemctl` and `journalctl` are invoked every few seconds,
  with read-only subcommands, to learn unit state and recent errors. No D-Bus
  library, no shell: arguments are passed directly.
- **Log files.** The file named by a running rclone's own `--log-file` argument
  — or by the unit that runs it, which is how a job on a timer is followed
  between its runs — is tailed, incrementally and from near its end: no file is
  opened that rclone was not already told to write, and nothing is ever written
  back to it. When that argument is a relative path it is resolved against the
  process's own working directory, read from `/proc/<pid>/cwd` — and when that
  cannot be read,
  which is the case for another user's process, the log is left alone rather
  than resolved against rclonetop's directory, where the same name would name a
  different file.
- **Wrapper scripts.** A unit that runs rclone from a shell script never names
  it, so the script itself is read to decide whether the unit is relevant, and
  to find the `--log-file` it passes. This is bounded to regular files under
  256 KiB that begin with a shebang, and only for units systemd already lists.
  A path built from a variable is followed only as far as a plain assignment
  and `$HOME` will take it, which is reading two lines rather than running
  them; command substitution, a default-value expansion or a variable that is
  never assigned all mean the log is left undiscovered.
- **rc endpoints.** Discovery reads command lines that are already on the host
  and never scans the network. rclone's own documentation notes that *access to
  the rc API is equivalent to shell access as the rclone user*.

## Status

Early. Six of the seven sources above work; the rc client is not written yet.
The command line and the configuration keys are expected to stay as they are,
but nothing is promised before 1.0.

Linux only for now — throughput is measured from `/proc/<pid>/io`, which has no
direct equivalent on macOS or the BSDs.

## Install

```sh
go install github.com/Seykhel/rclonetop/cmd/rclonetop@latest
```

The binary lands in `$(go env GOPATH)/bin`, which is not on `PATH` by default.
Either add it, or build to a directory of your choosing:

```sh
git clone https://github.com/Seykhel/rclonetop
cd rclonetop
go build -o ~/.local/bin/rclonetop ./cmd/rclonetop
```

## Usage

```
rclonetop [options]
```

| Option | Meaning |
|---|---|
| `--theme <name>` | colour theme; btop themes are found automatically |
| `--graph-symbol <s>` | graph glyphs: `braille`, `block` or `tty` |
| `--theme-background` | use the theme's background colour (default true) |
| `-u`, `--update <ms>` | refresh interval in milliseconds (default 2000) |
| `--base-10` | size units in KB=1000 instead of KiB=1024 |
| `-t`, `--tty` | force TTY mode: 8 colours, and ASCII graphs — but a `--theme` or `--graph-symbol` named alongside it still wins |
| `-l`, `--low-color` | limit output to 256 colours |
| `--no-alt-screen` | draw in place instead of on the alternate screen |
| `-c`, `--config <file>` | read this configuration file instead of searching |
| `--default-config` | print a commented default configuration, then exit |
| `-d`, `--debug` | print what each collector saw, then exit |
| `-h`, `--help` | show usage |
| `-V`, `--version` | show the version |

Keys: `q` or `Esc` to quit, `+` and `-` to refresh faster or slower.

The flags mirror btop's wherever the meaning is the same, so anything you have
already tuned there carries over. btop's `-p` and `--vim-keys` are not accepted
yet: they belong to the box presets and to having something on screen to move
between, neither of which is written. A flag that is accepted and ignored is
worse than one that is rejected, so they are not registered until they work.

## Configuration

Most of what the flags set can be set once in a file instead — everything that
is a setting rather than an action, which leaves out `-d`, `-h`, `-V`, `-c` and
`--default-config`, and for now `--no-alt-screen` as well:

```
$XDG_CONFIG_HOME/rclonetop/rclonetop.conf
~/.config/rclonetop/rclonetop.conf
```

The first that exists is read, and anything given on the command line overrides
it — including a flag whose value happens to equal the built-in default, which
is still you choosing it.

The format is btop's, and so are the key names wherever the meaning is the same,
so a setting you have already tuned in `btop.conf` reads the same here:

```
color_theme = "dracula"
theme_background = True
graph_symbol = ""
update_ms = 2000
base_10_sizes = False
force_tty = False
truecolor = True
clock_layout = "15:04:05"
```

rclonetop never writes that file. btop rewrites its own configuration on exit,
which is not compatible with reading only, so the commented default btop would
have written for you is printed instead and it is your shell that decides where
it lands:

```sh
rclonetop --default-config > ~/.config/rclonetop/rclonetop.conf
```

A key this version does not recognise is ignored rather than refused, so one
file can be shared between machines running different versions — the cost being
that a misspelled key is silently skipped too. A key it does recognise with a
value that cannot mean anything is an error naming the file and the line:
`rclonetop.conf:3: update_ms: "soon" is not a number`.

Two keys need a word of warning:

- `clock_layout` is btop's `clock_format` under another name, because the value
  is not the same thing: it is a Go reference layout — the time written out for
  `Mon Jan 2 15:04:05 MST 2006` — rather than a strftime string. Pasting `%X`
  from `btop.conf` is common enough that it, and the old key name, are both
  refused with the explanation rather than printed literally in the header.
- `graph_symbol` left empty is a third state rather than a fourth symbol: it
  means nobody has chosen. That matters because of how `force_tty` interacts
  with it, which is one rule: **naming a thing beats a flag that only implies
  it, within one source** — `force_tty` says something about the terminal and
  answers the font question by implication, while `graph_symbol` answers it
  outright — **and across sources the command line wins outright**, so a `--tty`
  typed at the prompt beats a symbol named in the file, like every other flag.
  `--theme` and `--tty` work the same way, with one difference worth knowing:
  `color_theme` has no "unchosen" state, so `force_tty` in a file still replaces
  that same file's theme. Only a `--theme` typed at the prompt survives it.

Because `force_tty` can be set in the file, there has to be a way to contradict
it at the prompt, and that is `--tty=false` — Go's flag syntax, and the reason
these rules ask both whether a flag was typed *and* what it was set to.

## Themes

rclonetop reads btop's `.theme` files directly — the same format bashtop and
bpytop use. Any theme installed for btop is available without copying anything:

```sh
rclonetop --theme dracula
rclonetop --theme gruvbox_dark
```

Themes are searched in this order:

```
$XDG_CONFIG_HOME/rclonetop/themes
$XDG_CONFIG_HOME/btop/themes
~/.config/rclonetop/themes
~/.config/btop/themes
/usr/local/share/rclonetop/themes
/usr/share/rclonetop/themes
/usr/local/share/btop/themes
/usr/share/btop/themes
```

Graphs are plotted directly rather than through a charting library: the
requirement is narrow — btop's braille cells, degrading to eighth-blocks and
then to plain ASCII for a Linux console — and expressing it takes less code than
adapting a general-purpose library would, with no dependency. Each graph scales
to the largest rate in its own window, and both directions of a transfer share
that scale so they stay comparable.

btop's colour vocabulary maps onto rclone closely enough to reuse as is:
`download` and `upload` keep their meaning, the memory ramps (`used`, `free`,
`cached`, `available`) describe remote and cache usage, the `cpu` ramp grades
transfer throughput, and `process` colours the files in flight. Two built-in
themes need no files: `default`, which reproduces btop's, and `tty` for
eight-colour consoles.

## Reporting a problem

`rclonetop -d` prints what every collector saw, including the ones that found
nothing. Paste that rather than a screenshot — it shows which source went quiet
and why.

## Building and testing

```sh
go build ./...
go test ./...
go vet ./...
```

The collectors are tested against fixtures rather than the live system: a fake
procfs on a temporary directory, canned `systemctl` and `journalctl` output, and
real log and listing formats captured from a working setup. The suite therefore
does not depend on what happens to be running.

Run it with `-race`. The process collector feeds unit ownership to the systemd
collector across a goroutine boundary, and that seam is covered by a test that
only fails under the race detector.

## Licence

Apache 2.0. See [LICENSE](LICENSE).

btop++ is by [Aristocratos](https://github.com/aristocratos) and is also
Apache 2.0; rclonetop reuses its theme format and configuration vocabulary, and
owes it the look.
