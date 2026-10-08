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
Execution of the other architecture requires a native runner; #50 adds that
release check.

The production command is the only release build target. A future demo
command remains a development tool and does not enter the archives.
GoReleaser prepares drafts if invoked for a release; #50 owns the tag-triggered
workflow and #51 owns the first manual publication.

GoReleaser's [Go builder](https://goreleaser.com/customization/builds/builders/go/)
and [archive documentation](https://goreleaser.com/customization/package/archives/)
describe the configuration fields used here.
