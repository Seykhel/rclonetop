#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

# Observe the deliverables, not GoReleaser's build configuration. In particular,
# an accidentally archived demo binary or missing attribution must fail here.
dist_dir=$(realpath "${1:-dist}")
# Draft downloads contain published assets, not GoReleaser's local metadata.
# Their expected version comes from the triggering tag instead.
version=${2:-}
if [[ -z "$version" ]]; then
  version=$(jq -er '.version | select(type == "string" and length > 0)' "$dist_dir/metadata.json")
fi
shopt -s nullglob
archives=("$dist_dir"/*.tar.gz)
[[ ${#archives[@]} -eq 2 ]] || { echo "Expected exactly two Linux archives" >&2; exit 1; }

expected_checksums=$(printf 'rclonetop_%s_linux_%s.tar.gz\n' "$version" amd64 "$version" arm64 | sort)
actual_checksums=$(awk '{print $2}' "$dist_dir/checksums.txt" | sort)
[[ "$actual_checksums" == "$expected_checksums" ]] || { echo "Unexpected checksum entries" >&2; exit 1; }
(cd "$dist_dir" && sha256sum --check checksums.txt)

package_tmp=$(mktemp -d)
trap 'rm -rf "$package_tmp"' EXIT
case $(uname -m) in
  x86_64) native_arch=amd64 ;;
  aarch64) native_arch=arm64 ;;
  *) echo "Run this check on Linux amd64 or arm64" >&2; exit 1 ;;
esac
expected_files=$(printf '%s\n' LICENSE NOTICE README.md rclonetop rclonetop.conf.example | sort)

for arch in amd64 arm64; do
  archive="$dist_dir/rclonetop_${version}_linux_${arch}.tar.gz"
  actual_files=$(tar -tzf "$archive" | sort)
  [[ "$actual_files" == "$expected_files" ]] || { echo "Unexpected contents in $archive" >&2; exit 1; }
  mkdir "$package_tmp/$arch"
  tar -xzf "$archive" -C "$package_tmp/$arch"
  binary="$package_tmp/$arch/rclonetop"
  [[ -x "$binary" ]] || { echo "Packaged binary is not executable" >&2; exit 1; }
  case $arch in
    amd64) machine='Advanced Micro Devices X86-64' ;;
    arm64) machine='AArch64' ;;
  esac
  readelf -h "$binary" | grep -F "Machine:" | grep -F "$machine" > /dev/null
  for document in LICENSE NOTICE README.md; do
    cmp "$document" "$package_tmp/$arch/$document"
  done
  [[ $(head -n 1 "$package_tmp/$arch/rclonetop.conf.example") == "#? Configuration file for rclonetop v$version" ]]
  if [[ $arch == "$native_arch" ]]; then
    [[ $("$binary" --version) == "rclonetop $version" ]]
    "$binary" --default-config > "$package_tmp/default-config"
    diff -u "$package_tmp/$arch/rclonetop.conf.example" "$package_tmp/default-config"
    echo "linux/$arch: extracted binary version and default configuration OK"
  else
    echo "linux/$arch: archive and ELF architecture OK (execution requires a native runner)"
  fi
done
