# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`rclonetop` is a read-only terminal monitor for rclone, styled after btop++. Go 1.25, Bubble Tea +
lipgloss, Linux only (throughput comes from `/proc/<pid>/io`). See `README.md` for the user-facing
description, the flag table and the theme search order.

## Commands

```sh
go build ./...
go vet ./...
go test -race ./...                                   # always -race, see below
go test ./internal/collect -run TestSystemdCollect    # a single test
go build -o bin/rclonetop ./cmd/rclonetop && ./bin/rclonetop -d
```

`-race` is not optional: facts are handed between collectors across goroutine boundaries —
`Procs.OnProcesses` → `Systemd.NoteProcesses` and `Logs.NoteProcesses`, `Systemd.OnLogFiles` →
`Logs.NoteUnitLogs`, `Logs.OnPaths` → `Bisync.NotePaths`. `TestConcurrentNoteAndCollect` and
`TestConcurrentNotePathsAndCollect` only fail under the detector.

Nor may a fixture depend on the wall clock. A journal fixture with a hard-coded epoch passed for a
day and then began failing on the clock rather than on the code, because retention drops an entry
older than `errorRetention`; anything asserting that an error is *kept* builds its timestamp from
`time.Now` (`recentEpoch` in `systemd_test.go`).

`-d` runs every collector twice, 500 ms apart, prints what each one saw and exits. It is the fastest
way to inspect collector output without a TTY, and it is what users are asked to paste in bug
reports. Because of that it is the one caller that survives a configuration file it cannot parse:
`main` warns on stderr and carries on with the defaults rather than returning the error, since a host
whose conf is broken is exactly the sort that gets reported and the diagnostic must not depend on the
thing being diagnosed.

`internal/ui.Version` holds the version string; there is no release tooling or CI in the tree yet
(`.github/workflows/` is empty). `/bin` and `/dist` are gitignored.

## Architecture

Three layers, one direction of flow:

```
internal/collect/*  →  model.Snapshot  →  model.State  →  model.View  →  internal/ui  →  terminal
   (goroutines)         (per source)       (per source)    (joined)      (Bubble Tea)
```

**Collectors** (`internal/collect`) each implement `Collector`: `Name`, `Source`, `Interval`,
`Available`, `Collect`. `collect.Run` starts one goroutine per *available* collector, each on its
own ticker, and funnels `Result` values into a single channel — a 5 s cache walk must never delay
the 1 s throughput sample. Intervals differ by design: procs 1 s, logs 2 s, localfs 5 s,
systemd 5 s, bisync 30 s. Collectors are registered in `cmd/rclonetop/main.go`, where the
cross-collector seams are also wired. The order of that slice is the order the facts travel in and
only matters for `-d`, which runs them in turn: processes name the units and the logs of what is
running now, units name the logs of what is not, and the logs name the paths the bisync listings
cannot spell. Any other order makes `-d` show each collector missing what the next was about to
tell it.

A collector that discovers its subject at run time (the log collector, from `--log-file` arguments)
must return `true` from `Available`. `collect.Run` filters once at startup, before any process has
been seen, so answering "nothing to do yet" there switches it off for the whole session.

**The model** (`internal/model`) is the shared vocabulary. Two invariants that are easy to break:

- Every value carries its `Source`. The UI distinguishes "measured", "inferred" and "this source
  went quiet" — a missing measurement must never be rendered as a zero, because a zero and an
  unreadable counter mean opposite things to someone checking whether their backup ran.
- In `State.Apply`, a **nil** slice means "this collector has nothing to say about that" and an
  **empty** one means "it looked and found none". Only the latter clears what is on screen. A
  collector that finds nothing must return an empty slice, not nil (`TestNoProcessesReportsEmptyNotNil`).
  `State.Fail` records an error without discarding earlier data.

`State.Apply` merges per-source: each collector owns the slices it fills, and one collector's
snapshot never overwrites another's. Where one collector holds a fact another *needs in order to
collect at all*, it is handed over at collection time rather than merged afterwards — see the seams
above. The bisync one is exact rather than a guess: `canonicalPath` mangles what the log said the way
rclone does and matches the result against the listing filename, because the mangling cannot be run
backwards.

**The joins** live in `State.Resolve` (`model/view.go`), which turns the per-source state into the
`View` a renderer draws: `ProcRow` and `UnitRow` carry the job, the timer and the errors that belong
to them, already matched on PID, unit name, log file and mountpoint. Rules that live here and
nowhere else: a unit whose process is on screen gets no line of its own but its journal errors move
to that process; two timers starting one service collapse to the one due first; a mount no process
serves is an orphan. `Resolve` takes no clock and returns plain data — ageing a timestamp for
display is the renderer's business — which is what makes these rules testable without rendering
anything (`model/view_test.go`).

**The UI** (`internal/ui`) is one Bubble Tea `Model`. `Update` handles four messages: window size,
keys, a clock `tick` (so uptimes advance with no new data), and `resultMsg` from the collector
channel — `waitFor` re-arms itself after each one. There is a single view, `renderDense` in
`dense.go`, plus `units.go`, `graphs.go`, `format.go` (`Bytes`/`Rate`/`Duration`/`Ago`/`Truncate`).
It renders a `View` and does no matching of its own: a renderer reaching back into `m.state` to find
something is the smell that a join has leaked back out of `Resolve`. Colour is applied only here, via
`Model.style(key)` and `Model.gradientStyle(ramp, frac)`.

**Sub-packages** deliberately kept free of colour and of Bubble Tea so they stay testable as plain
data: `internal/ui/graph` (braille / eighth-block / ASCII plotting, returns bare runes),
`internal/series` (fixed-capacity `Ring`, sized from the terminal width, drops non-finite samples),
`internal/theme` (btop `.theme` parsing, 101-step gradients matching btop's banding, plus the
`default` and `tty` built-ins in `builtin.go`).

`internal/execstart` is the other one: everything involved in reading what a unit actually runs,
behind two functions plus a helper — `DrivesRclone(execStart)`, `LogFile(execStart, home)` and
`LogFileFromArgs(args)`. Behind them sit systemd's `path=`/`argv[]=` spelling, the bounded wrapper
script read, and the shell reading that resolves `--log-file "$LOG_DIR/x.log"` without running
anything. The systemd collector keeps only the cache of the answers, and `Logs` uses
`LogFileFromArgs` on the vector it takes from `/proc`. `home` is what `$HOME` stands for and must be
empty when there is no honest answer — a system unit's home is root's, or whatever `User=` says.

**Configuration** (`internal/config`) parses btop's `key = value` format into a comparable `Config`.
It is read at startup by `cmd/rclonetop` and never afterwards, and it is never written: btop rewrites
its own configuration on exit, which rclonetop cannot do and stay read-only, so `--default-config`
prints the commented file btop would have written and the user's shell decides where it lands.

The rules that live there:

- **Precedence is decided by what was typed, not by what the value is.** `parseFlags` records the
  long form of every flag `flag.Visit` reports, and `applyConfig` lays the file under the command
  line by consulting that map. Comparing against the defaults instead would let the file overrule
  `--update 2000`, which is a user choosing the same number the default happens to be. For the same
  reason every flag with a key is registered with `config.Defaults()`'s value rather than a literal:
  a flag's default *is* what the file would have supplied, and one spelling cannot drift from two.
- **The file is forgiving where the prompt is strict**, and the split is between kinds of wrongness,
  not between sources. An unknown *key* is skipped, because a file written for a later version names
  boxes and presets this build has never heard of and refusing to start would break every downgrade;
  likewise an unknown `graph_symbol`, since the plotter falls back to braille. A known key with a
  value that cannot mean anything — a number that is not a number, an `update_ms` below the floor —
  is an error naming the file and the line. Skipping is not free and the cost is real: a misspelled
  key is skipped as silently as a future one, so `vim_keys = True` in a file does nothing and says
  nothing. That is the trade accepted, not a claim that the file warns.
- **`clock_format` is the one unknown key that is refused**, because it is the one that never will be
  a later version's: it is btop's name for a value rclonetop cannot take. rclonetop spells it
  `clock_layout` and means a Go reference layout, not a strftime string — `time.Format` would render
  `%X` literally and a header reading `%X` looks like a bug in rclonetop rather than a mistake in the
  file. Both the old name and a `%` in the value are refused with the explanation. This is also why
  the key was not simply called `clock_format`: btop's names are reused *where the meaning is the
  same*, and a value neither program can read is not the same meaning.
- **`graph_symbol` empty is a third state**, not a fourth symbol: it means nobody has chosen. Naming
  a symbol is a statement about the font and `force_tty` one about the terminal, so a named symbol
  beats `force_tty` — but only within one source; across sources the command line wins outright.
  `resolveGraphSymbol` in `cmd/rclonetop/flags.go` is the single place that settles it, and it must
  stay single: the first version asked only "is the symbol empty", which reads correctly until a
  default configuration file names braille, at which point `--tty` silently stops producing ASCII for
  everyone who copied one. `main` no longer decides this. The theme has the same shape one step
  further on — `force_tty` read from the file must not replace a `--theme` named at the prompt.
- `defaultFile` is prose written by hand — it is the only documentation of the format that ships with
  the binary — so it can drift from the defaults it claims to show. `TestDefaultFileRoundTrips` is
  the only thing that stops it: it parses the text and requires the result to equal `Defaults()`.
  That is also why `Config` has no slices or maps.

### Things that will bite

- **Rates need two samples.** `/proc/<pid>/io` counters are cumulative and must never be rendered as
  a rate; `ReadRate`/`WriteRate` are zero on the first sample, and the graph store drops it. When
  `IOAvailable` is false (process owned by another user) the UI shows a placeholder.
- **Only the process collector advances the graphs.** Sampling on every collector's tick would
  stretch the time axis by however many sources happen to be enabled.
- **`effectiveWidth`** resolves a reported width of 0 to 80. Every consumer must agree: the renderer
  treating 0 as 80 while graph sizing treated it as "too narrow" silently dropped the graphs.
- **A log file is append-only across runs.** One file holds every run of the job that writes it, so
  the parser has to notice where one ends and the next begins — bisync's "Synching Path1" line, or
  the elapsed time going backwards, which is the only marker a `sync` or `copy` gives. Without that
  this morning's failure is still reported as tonight's state. A statistics block is committed only
  when its "Elapsed time" line arrives: half a block is not a measurement.
- **systemd units are not simple.** A `Type=oneshot` sits at `activating` for its whole run and
  `inactive` afterwards whether it succeeded or not — hence `Unit.Running`, `Active`, `Failed` and
  `LastRun`. `ExitStatus` is an exit code only when `ExitCode` is `1` (CLD_EXITED); with `2` the same
  number is a signal. Journal errors are retained per unit (max 5, 24 h) and forgotten once a later
  run succeeds, because the tail is incremental and reporting only new lines would make a failure
  flash for one frame.

### Test seams

There is no `testdata/` directory: fixtures are built inline in `t.TempDir()` or embedded as string
constants, so the suite never depends on what is running on the host.

- `NewProcsAt(root)`, `NewBisyncAt(dir)`, `NewLocalFSAt(mountInfo, cacheRoot)` — filesystem roots.
- `newSystemdWith(run, scopes)` — injects a `runner func(ctx, name, args...) ([]byte, error)` in
  place of real `systemctl`/`journalctl`, fed canned JSON.
- `Logs` needs no seam of its own: it is told what to read by `NoteProcesses`, so a test points it
  at a file in `t.TempDir()` through a fabricated command line. `feed(lines...)` drives the parser
  alone.
- `internal/execstart` needs none either: its input is a string, and its one side effect is reading
  a path that string names, so `writeScript` puts a wrapper in `t.TempDir()` and hands back the
  `ExecStart` systemd would have recorded for it.

The log fixtures are transcribed from real rclone output, paths neutralised and nothing else
tidied — the tab inside `Transferred:`, the alignment padding, the trailing `Listed` count. Two of
them are load-bearing: the JSON entry's own text says 643.062 KiB where its `stats` object says
658496 bytes, which is what pins the preference for the object; and the mangled listing name in
`bisync_test.go` was produced by a real `rclone bisync` on a path containing a space.

Any new collector should follow the same shape: a real constructor plus an `...At`/`...With` variant.

## Constraints to preserve

- **Read-only, always.** No writes, no config changes, no starting or stopping transfers or units,
  no network scanning. `systemctl`/`journalctl` are invoked directly (no shell, no D-Bus library)
  with read-only subcommands and `LC_ALL=C`; wrapper-script reads (`internal/execstart`) are bounded
  to regular files under 256 KiB that start with a shebang, opened `O_NONBLOCK` and inspected through
  the descriptor, and only for units systemd already lists. A script is read, never run: command
  substitution, `${VAR:-default}` and an unassigned variable all mean "no answer", because working
  out what those produce means executing it.
- **No flag that does nothing.** btop's `-p` and `--vim-keys` are intentionally unregistered until
  the box presets exist and there is something on screen to move between; accepting and ignoring a
  flag is worse than rejecting it. Short flags mirror btop's meaning where it applies. The same rule
  binds `internal/config`: `shown_boxes`, `presets` and `vim_keys` get no `Config` field to be parsed
  into and shelved. A key read into a field that nothing consumes is the same lie as a flag, and
  harder to notice in a file than at a prompt. (Unrecognised keys are still skipped in silence, for
  the forward-compatibility reason below — that is a cost of the design, not a warning system.)
- **No new dependencies without a real reason.** Graphing and theme parsing are hand-written
  precisely because the requirement is narrower than any library's.
- Comments in this codebase explain *why*, at length, especially where the obvious implementation is
  wrong. Match that register when editing; do not strip the rationale.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `Seykhel/rclonetop`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical roles are used as-is: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root, created lazily. See `docs/agents/domain.md`.
