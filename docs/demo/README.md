# Recording the simulated demo

From the repository root, run `scripts/record-demo.sh`. It builds only the
separate `tools/demo` development command, then records `docs/demo/demo.gif`
using the committed VHS tape. Run `go run ./tools/demo` to explore it interactively;
press `p` to switch views and `q` to quit. No live collectors are started.

Prerequisites: Linux, the Go version in `go.mod`, VHS 0.10.0, ttyd 1.7.7,
FFmpeg, and the DejaVu Sans Mono font. Install VHS with
`go install github.com/charmbracelet/vhs@v0.10.0`; ensure the tools are on PATH.
VHS also needs Chromium (it can download its managed browser on first use).
Recording needs local browser/server execution and may need internet on first use.

The scenario emits fresh synthetic collector results once per second: a mount
and a 48 MiB copy, `report.pdf` in flight, then a successful job after 12 seconds.
The tape switches from dense to framed during the copy and back after completion.
The real UI handles the results, model joins, graphs and keyboard input.
Paths, PID values and host name are fictional. The theme is `vivid` with a fixed true-color profile, the terminal
is 120 columns by 32 rows, and the header clock reads `SIMULATED`; no host or
calendar timestamps enter the scenario. Timing is fixed, though browser startup
and frame encoding can vary between recording machines.

The test drives the same scenario through the UI's collector channel and keyboard
boundary without sleeps or time-sensitive assertions. Run `go test -race ./tools/demo`.

Review the generated animation before committing it: check progress and the file
row in dense view, graphs and progress in framed view, and the successful outcome
in both views. Regenerate manually when the UI or demonstrated behavior changes;
the recording is not part of release automation. Release archives contain only
the production command, never this demo executable.
