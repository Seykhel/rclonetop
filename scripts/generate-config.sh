#!/usr/bin/env bash
set -euo pipefail

# A fixed example version makes drift checks independent of the source version
# and of tags. Packaging supplies its own version so the bundled file says what
# binary wrote it.
version=${1:-0.0.0-example}
config_tmp=$(mktemp -d)
trap 'rm -rf "$config_tmp"' EXIT

go build -trimpath \
  -ldflags "-X github.com/Seykhel/rclonetop/internal/ui.Version=$version" \
  -o "$config_tmp/rclonetop" ./cmd/rclonetop

if [[ $# -ge 2 ]]; then
  mkdir -p "$(dirname "$2")"
  "$config_tmp/rclonetop" --default-config > "$2"
else
  "$config_tmp/rclonetop" --default-config
fi
