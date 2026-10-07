# Interactive help

Design confirmed during the 2026-10-07 interview.
This is the help slice of milestone #7.

## Purpose and scope

Provide an English reminder of every available keyboard command while the
monitor remains running. Include help rendering, footer hints, scrolling,
and documentation of the new keys.

## Interaction

- `?` or `h` opens help. Each opening starts at the beginning.
- `?`, `h`, or `Esc` closes help and returns to the monitor.
- `q` and `Ctrl+C` exit the application even while help is open.
- Outside help, `Esc` retains its existing quit behavior.
- While help is open, monitor commands (`p`, `P`, panel digits, interval keys)
  are consumed without changing the monitor. Collection, clock updates, and
  graph sampling continue normally.
- Up/Down scroll by line; Page Up/Page Down scroll by page. Scrolling stops
  at the beginning and end of the content.
- Resizing preserves the currently read command entry where possible.

## Content and presentation

Show all commands in groups, including the aliases accepted by the monitor.
Explain that digits 1–4 toggle transfers, bandwidth, files, and status in
the framed view. The opening view and a persistent hint explain how to close
help. This feature does not add a guide to measurements, colors, or sources.

Display a centered framed overlay when the complete content and its frame
fit. Otherwise use the full terminal, wrap text to its width, and scroll if
needed. Determine the transition from the rendered content's required space.
Use the current theme and console-compatible geometry. Keep actionable hints
legible and keep the help rendering within the terminal dimensions.

The monitor footer prioritizes whole elements in this order:

1. `? help`
2. `q quit`
3. View and preset commands
4. Update interval
5. Sources

Omit lower-priority elements when they do not fit rather than cutting hints
in half. On terminals too small for even one hint, clamp to the available
rectangle; the close and quit keys remain effective.

## Deferred work

Menu, filtering, detail views, monitor navigation, and `--vim-keys` remain
outside this issue. Help scrolling uses arrow and page keys.

## Verification

Verify opening, closing, quitting, blocked monitor commands, continued
collection and graph updates, complete command coverage, scrolling boundaries,
resize behavior, dense/framed parity, console rendering, and footer priorities.
Check narrow and short terminals, including dimensions too small for a hint.
Run formatting checks, `go build ./...`, `go vet ./...`, and
`go test -race -count=1 ./...` for the implementation.
