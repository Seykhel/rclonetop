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

`-race` is not optional: the process collector hands facts to two other collectors across a
goroutine boundary (`Procs.OnProcesses` → `Systemd.NoteProcesses` and `Logs.NoteProcesses`), and the
log collector hands paths to a third (`Logs.OnPaths` → `Bisync.NotePaths`).
`TestConcurrentNoteAndCollect` and `TestConcurrentNotePathsAndCollect` only fail under the detector.

`-d` runs every collector twice, 500 ms apart, prints what each one saw and exits. It is the fastest
way to inspect collector output without a TTY, and it is what users are asked to paste in bug
reports.

`internal/ui.Version` holds the version string; there is no release tooling or CI in the tree yet
(`.github/workflows/` is empty). `/bin` and `/dist` are gitignored.

## Architecture

Three layers, one direction of flow:

```
internal/collect/*  →  model.Snapshot  →  model.State  →  internal/ui  →  terminal
   (goroutines)         (per source)       (merged)        (Bubble Tea)
```

**Collectors** (`internal/collect`) each implement `Collector`: `Name`, `Source`, `Interval`,
`Available`, `Collect`. `collect.Run` starts one goroutine per *available* collector, each on its
own ticker, and funnels `Result` values into a single channel — a 5 s cache walk must never delay
the 1 s throughput sample. Intervals differ by design: procs 1 s, logs 2 s, localfs 5 s,
systemd 5 s, bisync 30 s. Collectors are registered in `cmd/rclonetop/main.go`, where the
cross-collector seams are also wired; the order of that slice is what makes `-d` useful, since the
process collector has to run before the log collector has anything to follow.

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

Merging is per-source: each collector owns the slices it fills. Cross-source merging on natural keys
(PID, unit name, the `(srcFs,dstFs)` pair) is deliberately not implemented yet. Where one collector
holds a fact another needs, it is handed over at collection time rather than merged afterwards —
see the seams above. The bisync one is exact rather than a guess: `canonicalPath` mangles what the
log said the way rclone does and matches the result against the listing filename, because the
mangling cannot be run backwards.

**The UI** (`internal/ui`) is one Bubble Tea `Model`. `Update` handles four messages: window size,
keys, a clock `tick` (so uptimes advance with no new data), and `resultMsg` from the collector
channel — `waitFor` re-arms itself after each one. There is a single view, `renderDense` in
`dense.go`, plus `units.go`, `graphs.go`, `format.go` (`Bytes`/`Rate`/`Duration`/`Ago`/`Truncate`).
Colour is applied only here, via `Model.style(key)` and `Model.gradientStyle(ramp, frac)`.

**Sub-packages** deliberately kept free of colour and of Bubble Tea so they stay testable as plain
data: `internal/ui/graph` (braille / eighth-block / ASCII plotting, returns bare runes),
`internal/series` (fixed-capacity `Ring`, sized from the terminal width, drops non-finite samples),
`internal/theme` (btop `.theme` parsing, 101-step gradients matching btop's banding, plus the
`default` and `tty` built-ins in `builtin.go`).

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

The log fixtures are transcribed from real rclone output, paths neutralised and nothing else
tidied — the tab inside `Transferred:`, the alignment padding, the trailing `Listed` count. Two of
them are load-bearing: the JSON entry's own text says 643.062 KiB where its `stats` object says
658496 bytes, which is what pins the preference for the object; and the mangled listing name in
`bisync_test.go` was produced by a real `rclone bisync` on a path containing a space.

Any new collector should follow the same shape: a real constructor plus an `...At`/`...With` variant.

## Constraints to preserve

- **Read-only, always.** No writes, no config changes, no starting or stopping transfers or units,
  no network scanning. `systemctl`/`journalctl` are invoked directly (no shell, no D-Bus library)
  with read-only subcommands and `LC_ALL=C`; wrapper-script reads are bounded to regular files under
  256 KiB that start with a shebang, and only for units systemd already lists.
- **No flag that does nothing.** btop's `-c`, `-p` and `--vim-keys` are intentionally unregistered
  until the config file and box presets exist; accepting and ignoring a flag is worse than rejecting
  it. Short flags mirror btop's meaning where it applies.
- **No new dependencies without a real reason.** Graphing and theme parsing are hand-written
  precisely because the requirement is narrower than any library's.
- Comments in this codebase explain *why*, at length, especially where the obvious implementation is
  wrong. Match that register when editing; do not strip the rationale.
