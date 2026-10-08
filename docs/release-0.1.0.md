# Release 0.1.0 — approved specification

Tracks [issue #8](https://github.com/Seykhel/rclonetop/issues/8).
The maintainer has confirmed the design scope, testing boundaries and ticket breakdown.
Published specification: [#47](https://github.com/Seykhel/rclonetop/issues/47).
Implementation tickets: [#48](https://github.com/Seykhel/rclonetop/issues/48)
(packaging), [#49](https://github.com/Seykhel/rclonetop/issues/49) (demo),
[#50](https://github.com/Seykhel/rclonetop/issues/50) (release workflow), and
[#51](https://github.com/Seykhel/rclonetop/issues/51) (maintainer publication).

## Agreed scope

- Complete milestone #8 through the first release, 0.1.0.
- Distribute Linux binaries for amd64 and arm64. Other operating systems are outside this milestone.
- Publish `.tar.gz` archives and checksums on GitHub Releases.
- Keep `go install` as an installation option. Distribution packages and an installer are deferred.
- A version tag triggers preparation of a draft GitHub Release, reviewed before manual publication.
- Each archive contains the binary, README, LICENSE, NOTICE and `rclonetop.conf.example`.
- Generate the example configuration with `--default-config` and check for drift in CI.
- Record a reproducible demo with synthetic data, explicitly presented as a demo,
  showing a transfer, a job outcome and switching between dense and framed views.
- Release the current feature set; menu, filtering, detail views and vim keys are not release prerequisites.
- Create the version tag manually after the implementation PR is merged and main CI passes.
- Before publication, smoke-test extracted release binaries on Linux amd64 and arm64:
  check the stamped version and `--default-config`. Keep the full race-enabled test suite
  and require a manual interactive check on amd64.
- Keep the demo scenario, recording script and VHS tape in the repository.
  Regenerate the recording manually when the UI or demonstrated sequence changes,
  rather than on every release.
- Implement the scenario in a separate `tools/demo` command feeding synthetic
  snapshots to the real UI. Exclude this command from release archives and do not
  add demo flags to the production binary. Record with a fixed terminal size and theme,
  and identify the data as simulated in the README.

## Existing groundwork

- Apache 2.0 licence, NOTICE and README already exist.
- CI already checks gofmt, build, vet and tests with the race detector.
  The unchecked CI item in #8 needs reconciliation.
- `internal/ui.Version` is a variable ready for build-time stamping;
  its source default is `0.1.0-dev`.

## Discussion status

All design questions raised in the interview have been answered.
The complete scope was confirmed by the maintainer.

## Problem Statement

Users currently need a Go toolchain to install rclonetop. The project has no repeatable
binary distribution or reviewed first-release process, and its README uses a hand-written
screen sample rather than a reproducible demonstration.

## Solution

Provide downloadable Linux archives for the current feature set, demonstrate that feature
set with simulated activity, and prepare a verified draft release for manual publication.

## User Stories

1. As a Linux amd64 user, I want a downloadable binary so that I can install without Go.
2. As a Linux arm64 user, I want the same distribution so that I can run on my architecture.
3. As a user, I want checksums so that I can verify downloaded archives.
4. As a user, I want the binary to report the released version so that bug reports identify my build.
5. As a user, I want licence and attribution documents bundled so that distribution terms are available offline.
6. As a user, I want a configuration example so that I can configure the monitor.
7. As a Go user, I want to retain go install so that my existing installation route remains available.
8. As a prospective user, I want to see progress and job outcomes so that I understand the monitor.
9. As a prospective user, I want both views demonstrated so that I understand their presentation.
10. As a maintainer, I want repeatable demo recording so that UI changes can be documented reliably.
11. As a maintainer, I want simulated demo data identified so that viewers understand the recording.
12. As a maintainer, I want packaging checked on PRs so that a broken distribution does not first appear at release time.
13. As a maintainer, I want configuration drift detected so that the example matches the binary.
14. As a maintainer, I want extracted binaries checked on both architectures so that release assets are usable.
15. As a maintainer, I want a draft release so that I can review assets before publication.
16. As a maintainer, I want explicit manual tag and publication steps so that release timing remains deliberate.

## Implementation Decisions

- Use GoReleaser to build only the production command for Linux amd64 and arm64,
  stamp its version and assemble archives plus checksums.
- Generate the configuration example through the production command's existing default-config action.
- Use the existing UI constructor and collector result input for a separate synthetic demo command.
  Keep rendering, model joins, graphs and keyboard handling on their real paths.
- Commit the recording source and output; document recording prerequisites and the simulated data.
- Validate packaging without publication on PRs; a manually created version tag prepares a draft.
- Check downloaded/extracted release archives on native runners for each supported architecture.
  Failure prevents manual readiness approval; no automated public publication occurs.
- Document the manual interactive amd64 check and final publication procedure.
- Keep CI's existing build, formatting, vet and race-enabled test requirements.

## Testing Decisions

- Test observable distribution behavior at the archive boundary: contents, checksum verification,
  execution on the target architecture, reported version and default configuration output.
- Compare the committed example with output from a build carrying the same version used to
  generate it; do not mistake a release version change in comments for configuration drift.
- Reuse the existing configuration round-trip tests as prior art; avoid tests duplicating parser implementation.
- Exercise the demo through the real UI result-input boundary and real keyboard commands,
  checking its defined scenario and transitions without live collectors or wall-clock-dependent assertions.
- Run the full Go suite with the race detector. Validate GoReleaser configuration and snapshot packaging.
- Review the generated recording and perform a manual interactive amd64 check before publication.

## Out of Scope

- Other operating systems, distribution packages and installers.
- New monitor features, production demo flags, live-host recording and automatic public releases.
- Regenerating the film on every release or changing the read-only runtime contract.

## Further Notes

- The parent milestone's CI checkbox is stale; reconcile it during milestone closeout.
- Preserve the README explanation about the Go installation directory not being on PATH by default.
- Final release execution is a maintainer task after implementation dependencies land.

## Proposed work split

1. Packaging: GoReleaser configuration, archive contents, stamped version,
   generated example configuration and CI drift check; validate packaging on PRs.
2. Demo: deterministic synthetic scenario and repeatable VHS recording;
   replace the README's hand-written sample with the recording.
3. Release workflow: tag-triggered draft, archive smoke tests on both architectures
   and documented manual review/publication steps.
4. First release: reconcile #8's checklist, merge prerequisites, check main CI,
   create `v0.1.0`, inspect the draft, perform the interactive check and publish.

The release workflow depends on packaging. First publication depends on all three
implementation tasks. The demo can be developed independently of packaging.
