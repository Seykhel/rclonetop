# rclonetop

Vocabulary for the rclone activity shown by the monitor.

## Language

**Selectable subject**:
A running rclone process or a systemd service represented by the monitor.
A service remains the same subject when its running process terminates.
_Avoid_: Selected row, selected panel

**Detail view**:
A live account of a selectable subject's current state and latest known
execution, using available measurements and retained errors.
_Avoid_: Run history, complete log

**Subject filter**:
A case-insensitive literal phrase that limits the monitor's processes and
services to subjects with a matching known name, path or remote.
_Avoid_: Log search, host-wide search

**Filter preview**:
The temporary set of matches shown while editing a subject filter, before
confirming it or returning to the previously applied filter.
