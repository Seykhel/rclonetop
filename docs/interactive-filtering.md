# Interactive filtering

Design for the filtering slice of milestone #7, agreed during the
2026-10-10 interview. The complete scope was confirmed before implementation.
Implementation follows the confirmed scope below.

## Agreed purpose

Find a selectable subject quickly among many rclone processes and systemd
services, then inspect it with the existing detail view.

Slash opens a search by name, path or remote. Filtering behaves consistently
in dense and framed views.

## Agreed scope and interaction

- Filter running processes and systemd service subjects across all panels.
  Keep bisync pairs, orphan mounts and caches visible as host context.
- Match plain text without case sensitivity. Do not add regular expressions
  or query syntax, and use only facts already collected.
- Slash opens editing with a live preview. Enter confirms; Escape restores
  the previously applied query. Printable keys, including q, h, j and k,
  are text while editing. Ctrl+C still quits.
- Keep filtering session-only, without new flags or configuration keys.
- Treat the complete query as a literal substring within one searchable field.
  Multiple words are one phrase, not separate conditions.
- Search command kind, service and timer names, known paths/remotes, log path
  and known bisync operands. Exclude complete argument vectors, error text,
  timestamps and measurements. Finished non-bisync services may lack their
  former operands; search only the available facts.
- If the preview excludes the selected subject, clear the selection without
  choosing another. Cancelling editing restores the initial selection if the
  subject still exists.
- In the monitor, Escape clears a committed nonempty filter before retaining
  its normal quit behavior when no filter is active. Confirming an empty query
  also clears filtering.
- Open filter editing only from the monitor. Help and detail keep their modal
  commands; close details before changing the query.
- Display the query and matching/total subject count. Distinguish no matches
  from no process running, so filtering cannot imply the host is idle.
- Reopening with Slash edits the current query with its cursor at the end.
- Support Left/Right, Home/End, Backspace/Delete and Ctrl+U to clear the
  editing buffer. Do not add history or completion. Up/Down do not navigate
  subjects while editing, and monitor commands remain suspended.
- Ignore leading and trailing query whitespace when matching; preserve internal
  spaces. A whitespace-only query behaves as an empty filter.
- Recompute matches and counts on collector updates, including during editing.
  Collection and graph history continue for excluded subjects too.
- An open detail view remains live if its subject stops matching but still
  exists. Returning to the monitor leaves no selection for that excluded
  subject. Actual disappearance retains the existing unavailable-subject
  behavior.
- Use the existing two footer rows, without reducing panel space. A committed
  filter replaces the separator rule. While editing, the prompt replaces the
  last row's monitor hints so it remains accessible in short terminals.
- Support Unicode editing without splitting characters. Pasted text remains
  text; pasted line breaks and tabs become spaces and do not confirm the query
  or dispatch monitor commands.
- Once preview matching excludes a selection, keep it cleared even if later
  edits include that subject again. Only cancelling editing restores the
  initial selection when still available, using the existing stable identity
  and process-alias rules through collector updates.
- When space is insufficient, prioritize text near the editing cursor; outside
  editing, prioritize the indication that a filter is active. Abbreviate the
  committed query and omit counts and hints when necessary. With only one row
  available, filtering takes precedence over monitor hints; commands remain
  effective. Do not let a filtered view look unfiltered.

## Matching and state boundaries

Filter one common projection of the resolved monitor subjects for rendering,
panel demand, selection navigation and opening details. Preserve the existing
ordering within the matching subset. Count matching and total subjects before
panel visibility or clipping, so hidden subjects remain reachable.

Keep the full resolved state for subject identity reconciliation, collector
health and detail evidence. Exclusion by a filter is distinct from actual
disappearance. Filtering does not remove collected facts, change collector
cadence or stop storing graph samples.

Entering editing captures the previous applied query and selection. Enter
commits the preview; Escape cancels it. Clearing an applied filter widens the
available subjects without automatically selecting one. With no matches,
navigation and Enter do nothing. Help and detail retain their existing modal
routing, including global quit behavior outside filter text editing.

## Verification

Use the agreed resolved-model and UI Update/View test boundaries. Exercise
literal phrase matching, case-insensitivity, outer versus internal whitespace,
available versus missing operands, and the exact searchable field set.
Verify identical matches across dense/framed views and every subject fragment,
unchanged nonselectable host context, counts before hidden/clipped panels,
source health and truthful empty-state messages.

Cover opening/reopening, editing cursor motion/deletion, Unicode, paste,
confirmation/cancellation, text containing monitor hotkeys, Vim aliases,
Ctrl+C, successive Escape behavior and opt-in modal entry. Verify selection
clearing without automatic reselection, cancellation restoring stable identity,
live data changing matches, true disappearance, details remaining readable for
excluded subjects, and continued collection and graph sampling.

Check long queries, horizontal cursor visibility, resizing, console rendering
and extreme terminal sizes. Confirm filtering preserves the framed height
budget and default footer behavior when inactive.

Implementation checks: gofmt, go build ./..., go vet ./..., and
go test -race -count=1 ./.... Update keyboard help and README with the actual
filter interaction. This slice adds no configuration or packaging format.
