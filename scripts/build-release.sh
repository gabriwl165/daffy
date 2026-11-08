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
hash_files() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@";
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$@";
  else echo "sha256sum or shasum is required" >&2; return 1; fi
}
commit=${GITHUB_SHA:-$(git -C "$root" rev-parse HEAD)}
case "$commit" in ''|*[!0-9a-fA-F]*) echo "invalid commit SHA" >&2; exit 1;; esac
[ "${#commit}" -eq 40 ] || { echo "commit SHA must be 40 characters" >&2; exit 1; }
for target in linux_amd64 linux_arm64 darwin_arm64; do
  case "$target" in
    linux_amd64) goos=linux; goarch=amd64;;
    linux_arm64) goos=linux; goarch=arm64;;
    darwin_arm64) goos=darwin; goarch=arm64;;
  esac
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "-X main.version=$commit" -o "$tmp/daffy" ./cmd/daffy
  tar -czf "$out/daffy_${target}.tar.gz" -C "$tmp" daffy
done
cp "$root/setup.sh" "$out/setup.sh"
(cd "$out" && hash_files daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz daffy_darwin_arm64.tar.gz setup.sh > SHA256SUMS)
