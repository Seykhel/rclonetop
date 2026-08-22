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

SYNC   home_user_Documents ⇄ gdrive_Documents
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
| rclone logs | job progress from `--log-file`, plain or `--use-json-log` | planned |
| rc API | exact statistics, when a daemon does expose it | planned |

Sources are independent. Whatever is unavailable is hidden rather than shown as
zero: a zero and an unreadable counter mean very different things to someone
checking whether their backup ran.

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
- **Wrapper scripts.** A unit that runs rclone from a shell script never names
  it, so the script itself is read to decide whether the unit is relevant. This
  is bounded to regular files under 256 KiB that begin with a shebang, and only
  for units systemd already lists.
- **rc endpoints.** Discovery reads command lines that are already on the host
  and never scans the network. rclone's own documentation notes that *access to
  the rc API is equivalent to shell access as the rclone user*.

## Status

Early. Five of the seven sources above work; the log parser and the rc client
are not written yet. The command line and the configuration keys are expected to
stay as they are, but nothing is promised before 1.0.

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
| `-t`, `--tty` | force TTY mode: 8 colours, and ASCII graphs unless `--graph-symbol` says otherwise |
| `-l`, `--low-color` | limit output to 256 colours |
| `--no-alt-screen` | draw in place instead of on the alternate screen |
| `-d`, `--debug` | print what each collector saw, then exit |
| `-h`, `--help` | show usage |
| `-V`, `--version` | show the version |

Keys: `q` or `Esc` to quit, `+` and `-` to refresh faster or slower.

The flags mirror btop's wherever the meaning is the same, so anything you have
already tuned there carries over. btop's `-c`, `-p` and `--vim-keys` are not
accepted yet: they belong to the configuration file and the box presets, which
are not written. A flag that is accepted and ignored is worse than one that is
rejected, so they are not registered until they work.

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
