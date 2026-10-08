#!/usr/bin/env bash
set -euo pipefail

config_check=$(mktemp)
trap 'rm -f "$config_check"' EXIT
bash scripts/generate-config.sh > "$config_check"
diff -u rclonetop.conf.example "$config_check"
