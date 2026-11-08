#!/bin/sh
set -eu
out=${1:-}
[ -n "$out" ] || { echo "usage: $0 OUTPUT_DIR" >&2; exit 2; }
mkdir -p "$out"
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
out=$(CDPATH= cd -- "$out" && pwd)
cd "$root"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
commit=${GITHUB_SHA:-$(git -C "$root" rev-parse HEAD)}
case "$commit" in ''|*[!0-9a-fA-F]*) echo "invalid commit SHA" >&2; exit 1;; esac
[ "${#commit}" -eq 40 ] || { echo "commit SHA must be 40 characters" >&2; exit 1; }
for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-X main.version=$commit" -o "$tmp/daffy" ./cmd/daffy
  tar -czf "$out/daffy_linux_${arch}.tar.gz" -C "$tmp" daffy
done
cp "$root/setup.sh" "$out/setup.sh"
(cd "$out" && sha256sum daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz setup.sh > SHA256SUMS)
