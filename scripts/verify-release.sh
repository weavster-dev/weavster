#!/usr/bin/env bash
# Verify the release archives scripts/release.sh wrote (CI and the release
# workflow run it before anything is published):
#
#   scripts/verify-release.sh VERSION DIR
#
# Every archive matches the checksums file and holds a CGO_ENABLED=0 binary
# with the license and readme; Linux binaries are statically linked; the
# binary for this machine (linux/amd64 in CI) must report VERSION, which
# checks the stamp (-trimpath keeps -ldflags out of the build info).
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: scripts/verify-release.sh VERSION DIR" >&2
  exit 2
fi
version=$1
cd "$2"

if command -v sha256sum >/dev/null; then
  sha256sum -c "weavster_${version}_checksums.txt"
else
  shasum -a 256 -c "weavster_${version}_checksums.txt"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
here="$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')"
count=0
for archive in weavster_"${version}"_*.tar.gz; do
  name=${archive%.tar.gz}
  tar -xzf "$archive" -C "$work"
  bin=$work/$name/weavster
  for f in weavster LICENSE README.md; do
    [[ -f $work/$name/$f ]] || { echo "::error::$archive lacks $f"; exit 1; }
  done
  if [[ -n $(tar -tzf "$archive" | grep '/\._') ]]; then
    echo "::error::$archive holds macOS metadata files"
    exit 1
  fi
  go version -m "$bin" | grep -q 'CGO_ENABLED=0' || { echo "::error::$name: not built with CGO_ENABLED=0"; exit 1; }
  if [[ $name == *_linux_* ]] && command -v file >/dev/null; then
    file "$bin" | grep -q 'statically linked' || { echo "::error::$name: not statically linked"; exit 1; }
  fi
  if [[ $name == "weavster_${version}_$here" ]]; then
    "$bin" version | grep -q "^weavster $version " || { echo "::error::$name: reports another version"; exit 1; }
  fi
  count=$((count + 1))
done
if [[ $count -ne 3 ]]; then
  echo "::error::want 3 archives, found $count"
  exit 1
fi
if [[ ! -f weavster_${version}_$here.tar.gz ]]; then
  echo "note: no archive for this machine ($here); the stamped version was not run"
fi
echo "verified $count archives of weavster $version"
