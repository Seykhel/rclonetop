#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
for tool in go vhs ttyd ffmpeg; do
  if ! command -v "$tool" >/dev/null; then
    echo "Missing recording prerequisite: $tool (see docs/demo/README.md)" >&2
    exit 1
  fi
done
mkdir -p bin
go build -o bin/rclonetop-demo ./tools/demo
vhs docs/demo/demo.tape
