# Selection and detail view

Design for milestone #7, agreed during the 2026-10-10 interview.
The complete scope was confirmed before implementation. Tests use the agreed
boundaries: resolved model, UI Update/View, and configuration/flag parsing.

## Agreed purpose

The next slice adds selection of a monitor entry and access to its details.
Its guiding scenario is: “The backup did not go as expected; I want to
understand the latest run and read the relevant errors.” This includes runs
whose process has already terminated.

Menu and filtering will be considered after the subject and purpose of the
detail view are clear.

## Agreed scope

- Select running rclone processes and systemd service entries already
  represented by the monitor. Retained manual jobs whose process has
  disappeared are outside this slice.
- Describe the current state and latest known run, without adding run history.
  Missing data and stale sources must remain distinguishable. A last-read
  log timestamp must not be presented as the run's completion time.
- Use information and errors already collected, identifying their sources.
  This slice does not add a complete log reader.

## Agreed interaction

- Selection follows the subject rather than its row position. A systemd
  service stays selected across its running-process and stopped-unit
  representations when the association is certain. User and system units
  with the same name are distinct; ambiguous associations must not be guessed.
- Details update live, including when a new run of the selected service
  begins. Available state and timestamps make changes apparent without
  inventing run boundaries the collected data cannot establish.
- Enter opens details. Use a framed overlay when it fits and the full
  terminal otherwise, following the help presentation. Long content scrolls.
  Escape returns to the monitor; collection and updates continue.
- Up/Down traverse a single sequence: processes followed by units, each in
  displayed order. The same selection model applies to dense and framed
  views. Fragments of one process in several panels are one selectable subject.
- Start without a selection. The first Down selects the first subject and
  the first Up selects the last. Subsequent updates preserve subject identity
  rather than its position in the ordering.
- If the subject disappears without an equivalent representation, clear the
  monitor selection. An open detail view reports that the subject is no longer
  available and does not present old facts as current. Escape returns normally.
- All subjects remain reachable when panels are hidden or content is truncated.
  The footer identifies the selected subject and offers Enter for details;
  visible representations are highlighted too. Selection does not change
  presets or panel visibility.
- Detail content covers identity, state/outcome, times and scheduling,
  available statistics, retained errors and source health. Include reliably
  associated service/timer facts for running processes too. Error messages
  remain readable in full through wrapping and scrolling.
- While details are open, monitor commands are consumed. Up/Down and Page
  Up/Down scroll; Escape closes; q and Ctrl+C quit. Return to the monitor to
  select a different subject.
- Add --vim-keys: j/k alias Down/Up in the monitor, details and help. Arrow
  keys always work. h remains help; this interaction has no horizontal motion.
- Navigation stops at either end without wrapping. With no subjects, arrows
  and Enter do nothing.
- Each detail opening starts at the top. Live updates and resizing preserve
  the section and reading position where possible, clamping to the new content
  bounds when it shrinks.
- With a selection, footer space prioritizes its identity and Enter details
  over less relevant elements. Abbreviate the name when necessary. Closing
  and quitting remain effective even at extreme terminal dimensions.
- ? or h opens help from details. Closing that help returns to details with
  its reading position preserved. q and Ctrl+C still quit globally.
- Add vim_keys = false to configuration. --vim-keys takes precedence using
  the existing explicit-flag rules, including --vim-keys=false overriding a
  configuration file that enables it.

## Model requirements

Selection must use stable subject identity, not a row index, display label,
or log path. Processes need protection against PID reuse; services need scope
as well as name. Existing name-only service joins cannot establish ownership
when user and system services share a name. Carry unambiguous ownership and
associated service/timer facts through the resolved model before rendering.
Do not create a second matching implementation in the UI.

Detail data comes from the resolved model, including source availability and
known/unknown distinctions. Existing error retention and partial log reads
mean the detail is an account of retained evidence, not a complete log.

## Verification

Cover stable selection under reordering, PID reuse, scope collisions,
unambiguous process/service transitions, disappearance and empty lists.
Verify dense/framed parity, selection with hidden or truncated panels,
footer priorities, console rendering and extreme terminal dimensions.
Exercise detail scrolling, wrapping, resizing, live updates, new runs,
source failures, missing measurements, nested help and keyboard routing.
Verify collection and graph sampling continue in details and help, plus
Vim aliases and configuration/explicit-flag precedence.

Implementation checks: gofmt, go build ./..., go vet ./..., and
go test -race -count=1 ./.... Regenerate the shipped configuration example
and run the relevant packaging checks described in docs/packaging.md when
adding vim_keys.
