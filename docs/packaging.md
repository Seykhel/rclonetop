# Packaging

Run these commands from the repository root on Linux. Use Go from `go.mod`,
GoReleaser v2.18.2, Bash, jq and binutils (`readelf`). CI uses the same
GoReleaser version and checks on pull requests.

## Configuration example

The checked-in example comes from the production binary's `--default-config`
output. Generate it after changing the configuration:

```sh
bash scripts/generate-config.sh 0.0.0-example rclonetop.conf.example
bash scripts/check-config-example.sh
```

The fixed `0.0.0-example` version makes the comparison independent of source
version changes and release tags. For archives, the GoReleaser before hook
generates a separate copy under `bin/`, stamped with the archive's version.
The runtime keeps its read-only behavior; these development commands write
the generated files.

## Local distribution check

```sh
goreleaser check
goreleaser release --snapshot --clean
bash scripts/check-packaging.sh
```

Snapshot mode prepares archives without publishing. It works before the
first tag exists; the version includes a snapshot suffix and commit identifier.
The output goes to the gitignored `dist/` directory. The check verifies the
two Linux archives, SHA-256 checksums, exact contents, executable permissions,
ELF architectures and bundled documents. It runs the extracted native binary
to verify `--version` and compare `--default-config` with the bundled example.
Execution of the other architecture requires a native runner; the release
workflow runs this check on both native architectures.

The production command is the only release build target. The demo command
remains a development tool and does not enter the archives.

## Prepare and publish a release

The maintainer creates a version tag only after the implementation PRs merge
and CI passes on the intended `main` commit. For the first release:

```sh
git switch main
git pull --ff-only
git tag v0.1.0
git push origin v0.1.0
```

The `Release` workflow repeats build, formatting, vet, race-enabled tests and
configuration drift checks, then uses GoReleaser v2.18.2 to prepare a **draft**.
Its draft job needs `contents: write` through `GITHUB_TOKEN` to upload release
assets. The smoke jobs also need `contents: write` for draft visibility
(GitHub restricts drafts to callers with push access), but only read assets;
the readiness job uses `contents: read`. Repository Actions settings must
allow these permissions and GitHub-hosted Linux amd64 and arm64 runners.

Each native runner downloads both archives and checksums from that draft,
checks the archive contents and checksums, and executes its extracted native
production binary to verify the tag's version and `--default-config` against
the bundled example. No smoke-test job compiles a replacement binary.
The `ready` job succeeds only after both architectures pass; it records
automatic verification without publishing anything.

If any build or smoke test fails, the release is unapproved. Leave it as a
draft, investigate the failed job and rerun verification after correcting the
problem. Do not publish partially verified assets or move an existing tag to
another commit; use a new version tag when a source correction is required.

Before publishing, the maintainer must:

1. Confirm the tagged commit is the intended release and the entire `Release`
   workflow, including both native smoke tests and `ready`, succeeded.
2. Review the draft notes, the two Linux archives and `checksums.txt`. Download
   the assets and inspect the bundled README, LICENSE, NOTICE and configuration.
3. On Linux amd64, extract the downloaded archive and launch `./rclonetop` in
   a terminal. Check both views with `p`, help with `h`, and quit with `q`.
   Confirm the display and available collectors behave correctly on the host.
4. Record these manual checks in the release notes, then publish the draft
   through GitHub's release editor. Publication is always a maintainer action.

For milestone #8, reconcile its completed packaging, CI, demo and configuration
checkboxes during #51 closeout; mark the tag complete only after publication.

GoReleaser's [Go builder](https://goreleaser.com/customization/builds/builders/go/)
and [archive documentation](https://goreleaser.com/customization/package/archives/)
describe the configuration fields used here.
