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

`-race` is not optional: the process collector hands unit ownership to the systemd collector across
a goroutine boundary (`Procs.OnProcesses` → `Systemd.NoteProcesses`), and
`TestConcurrentNoteAndCollect` in `internal/collect/systemd_test.go` only fails under the detector.

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
the 1 s throughput sample. Intervals differ by design: procs 1 s, localfs 5 s, systemd 5 s,
bisync 30 s. Collectors are registered in `cmd/rclonetop/main.go`.

**The model** (`internal/model`) is the shared vocabulary. Two invariants that are easy to break:

- Every value carries its `Source`. The UI distinguishes "measured", "inferred" and "this source
  went quiet" — a missing measurement must never be rendered as a zero, because a zero and an
  unreadable counter mean opposite things to someone checking whether their backup ran.
- In `State.Apply`, a **nil** slice means "this collector has nothing to say about that" and an
  **empty** one means "it looked and found none". Only the latter clears what is on screen. A
  collector that finds nothing must return an empty slice, not nil (`TestNoProcessesReportsEmptyNotNil`).
  `State.Fail` records an error without discarding earlier data.

Merging is per-source: each collector owns the slices it fills. Cross-source merging on natural keys
(PID, unit name, the `(srcFs,dstFs)` pair) is deliberately not implemented yet.

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
