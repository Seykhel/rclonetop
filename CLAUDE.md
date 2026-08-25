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
reports. It opens with a `== display` block — colour profile, theme, graph symbol, `TERM` and
`COLORTERM` — because "the colours look washed out" is the one complaint a collector dump cannot
otherwise answer. It also says when stdout is not a terminal, and that caveat is load-bearing:
termenv correctly reports `Ascii` for a pipe, and a user pasting `-d` into a bug report has
*always* piped it, so the bare profile line would confidently answer the wrong question.

Because of that it is the one caller that survives a configuration file it cannot parse:
`main` warns on stderr and carries on with the defaults rather than returning the error, since a host
whose conf is broken is exactly the sort that gets reported and the diagnostic must not depend on the
thing being diagnosed.

`internal/ui.Version` holds the version string; there is no release tooling in the tree yet. CI is
`.github/workflows/ci.yml`: a single job running gofmt, `go build`, `go vet` and
`go test -race -count=1`, with the Go version read from `go.mod` so the two cannot drift. It is the
same four commands as above, which is the point — a change that passes locally passes there.
`/bin` and `/dist` are gitignored.

## Changes go through a pull request

**`main` is never pushed to directly.** Branch, push the branch, open a PR:

```sh
git switch -c <topic>
gh pr create --fill                 # see docs/agents/issue-tracker.md for the gh conventions
```

CI triggers on `pull_request` as well as on pushes to `main`, so the checks are the same either way
— but on a branch they run *before* the commit becomes one somebody may bisect, which is the whole
reason the rule exists.

Committing to `main` by mistake costs nothing as long as it has not been pushed, and it is worth
knowing the way out rather than rebuilding the work:

```sh
git switch -c <topic>               # the branch now holds the commits
git branch -f main origin/main      # and main goes back to where it was
```

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
something is the smell that a join has leaked back out of `Resolve`. Colour is applied only here.

**A ramp may be indexed at a point somebody chose; it must never be indexed by a measurement.** This
is the one rule of the colour vocabulary. It was first written as "raw for area, blended for text",
which is the wrong cut and cost a round of review: it washed out fixed accents that were already
legible. The line is **measured versus chosen**, because only a measurement reaches zero, and the
zero end of a btop ramp is unreadable.

- `Model.magnitudeStyle(ramp, frac)` — `frac` is a **measurement**: a rate over the observed peak,
  RSS over 1 GiB, a job's completion, how stale a run is. It blends from `main_fg` *towards* the ramp
  rather than indexing it, because an idle mount sits at `frac` 0 for hours and indexing wrote
  `↓ 0 B/s` in near-black violet. At 0 the text is plain `main_fg`; at 1 it is the ramp's own hot
  end, so nothing is lost at the top.
- `Model.accentStyle(a)` — `a` is a **chosen** point, declared in `textAccents`: "cache figures are
  cyan". Indexed raw, because blending only dilutes a colour somebody already picked for being
  legible. An accent not listed in `textAccents` escapes the legibility test, which is the one way to
  get an unreadable one past the suite.
- `Model.gradientStyle(ramp, frac)` — raw, and reached for directly only to fill **area**: the
  sparkline, and meters when they exist. A dark *cell* against a dark background honestly reads as
  "not much"; a dark glyph reads as nothing at all.

Three tests hold it, and the third is the one that matters. `TestTextStaysLegibleAcrossTheWholeRamp`
and `TestFixedAccentsAreLegible` hold both text paths above half of `main_fg`'s luminance;
`TestTheRawRampReallyIsTooDarkForText` pins the *premise*, so the cure cannot be "simplified" back
into the disease with everything else still green. Luminance is Rec. 709 and is a poor proxy for a
saturated primary — it scores `#ff0000` at 54 — which is why the accent test runs against the
built-in theme only and the `tty` one is exempt.

**Three levels of emphasis, and each boundary was a mistake on one side of it.** `Model.label()` is
halfway between `main_fg` and `inactive_fg`; `Model.value()` is `main_fg` and **not bold**. Labels
were `inactive_fg` (`#40`, the colour that means *switched off*), which made most of the screen
invisible and left nothing to say "switched off" with. Made `main_fg` instead, they were
indistinguishable from the figures they name. And bolding every value made the screen uniformly
bright and flat — emphasising everything emphasises nothing.

**Bold rides with colour, and the styles carry it themselves.** `magnitudeStyle`, `accentStyle` and
`alarm` come back bold; `gradientStyle` does not, because area has no glyph to thicken. Do not add
`.Bold(true)` at a call site: that is how this started, as a sentence in this file plus a
`.Bold(true)` repeated fourteen times with not one caller wanting otherwise. A rule with no
exceptions belongs in the constructor, and `TestWeightIsBuiltIntoTheColouredStyles` keeps it there.

`inactive_fg` is only for what is genuinely inert or stale: "collecting…", "throughput unavailable",
"idle", a staleness note. Not for a permanent key hint — `q quit` is chrome that is always actionable
and takes `label()`, which is the trap this reservation exists to catch.

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
- **Naming a thing beats a flag that only implies it, within one source; across sources the command
  line wins outright.** One rule, settled in two places and nowhere else: `resolveGraphSymbol` and
  `resolveTTYTheme` in `cmd/rclonetop/flags.go`. `--tty` says "this is a console" and answers the
  font and the palette by implication; `--graph-symbol` and `--theme` answer them outright, so both
  survive a `--tty` typed alongside them. Both resolvers run *after* `applyConfig` has settled
  `o.tty`, and read that rather than `cfg.ForceTTY` — moving either call up would compile and would
  quietly start ignoring `--tty=false`.
  - `graph_symbol` empty is a third state, not a fourth symbol: it means nobody has chosen, which is
    what lets the file distinguish a symbol it named from one it defaulted to. `color_theme` has no
    such state — it always holds something, and `default` is a theme somebody may have meant — so
    `force_tty` read from a file still replaces the file's own theme. Only the prompt can be sure.
  - `--tty=false` is the only way to say "no, this is not a console" when the file says it is, so it
    has to work. Two attempts did not. The first asked only "is the symbol empty", which reads
    correctly until a default configuration file names braille, at which point `--tty` silently stops
    producing ASCII for everyone who copied one. The second asked `o.explicit[flagTTY]` alone, which
    reads correctly until somebody types `--tty=false` — a flag that was typed, and is off. **A
    boolean flag has three states and `flag.Visit` distinguishes two of them**: any rule weighing two
    options has to ask both whether it was typed and what it says.
- **The `explicit` map is keyed by constants** (`flagTTY`, `flagTheme`, …) and is not read outside
  `flags.go`. A typo in one of those strings compiles, passes every test that happens not to cover
  that option, and switches off its precedence rule — the file just starts winning against a typed
  flag, which is the one thing the mechanism exists to prevent.
  `TestExplicitKeysAreRealFlagNames` types every one of them.
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
  when its "Elapsed time" line arrives: half a block is not a measurement. The file lists rclone
  writes *after* that line — `Checking:` and then `Transferring:` — belong to the block it has just
  committed, so they are gathered and attached when the list ends rather than reopening it, on the
  same grounds: half a list is not a measurement either. `Job.Transferring` keeps the same
  nil-versus-empty distinction as the rest of the state, and the two are both reachable — nil is a
  run under `--stats-one-line`, empty is a run between two files.
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
  precisely because the requirement is narrower than any library's. `github.com/charmbracelet/x/term`
  is a direct require and was not a new dependency when it became one: bubbletea already links it to
  take the alternate screen, so `go mod tidy` moved one line and added no code. It answers "is stdout
  a terminal" for `-d`, and it is there because the hand-rolled version was *wrong* — a character
  device is not a tty, `/dev/null` is one too, and the comment asserting otherwise was believed for
  exactly as long as nobody checked. `term.IsTerminal` is a real `ioctl(TCGETS)`.
- Comments in this codebase explain *why*, at length, especially where the obvious implementation is
  wrong. Match that register when editing; do not strip the rationale.

## Agent skills

### Issue tracker

Issues live in GitHub Issues on `Seykhel/rclonetop`, via the `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

The five canonical roles are used as-is: `needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`. See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` and `docs/adr/` at the repo root, created lazily. See `docs/agents/domain.md`.
