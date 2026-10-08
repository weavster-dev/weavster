#!/usr/bin/env bash
# Build the release archives: a static binary for each target, packed with
# the license and readme, and a SHA-256 checksums file (D-55: no signing).
#
#   scripts/release.sh VERSION OUTDIR
#
# VERSION is the release version without the leading v (1.2.0). OUTDIR gets
# weavster_<VERSION>_<os>_<arch>.tar.gz for each target and
# weavster_<VERSION>_checksums.txt, which `sha256sum -c` verifies.
set -euo pipefail

if [[ $# -ne 2 || ! $1 =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: scripts/release.sh VERSION OUTDIR (VERSION like 1.2.0 or 1.2.0-rc.1)" >&2
  exit 2
fi
version=$1
out=$2
targets=(linux/amd64 linux/arm64 darwin/arm64)
date=$(date -u +%Y-%m-%dT%H:%M:%SZ)

mkdir -p "$out"
out=$(cd "$out" && pwd)
root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cd "$root"
for target in "${targets[@]}"; do
  os=${target%/*}
  arch=${target#*/}
  name=weavster_${version}_${os}_${arch}
  mkdir -p "$work/$name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$version -X main.buildDate=$date" \
    -o "$work/$name/weavster" ./cmd/weavster
  cp LICENSE README.md "$work/$name/"
  # No macOS metadata (._* files, extended attributes) in the archive.
  COPYFILE_DISABLE=1 tar --no-xattrs -C "$work" -czf "$out/$name.tar.gz" "$name"
done

cd "$out"
if command -v sha256sum >/dev/null; then
  sha256sum weavster_"${version}"_*.tar.gz >"weavster_${version}_checksums.txt"
else
  shasum -a 256 weavster_"${version}"_*.tar.gz >"weavster_${version}_checksums.txt"
fi
cat "weavster_${version}_checksums.txt"
