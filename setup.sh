#!/bin/sh
set -eu
fail() { echo "daffy installer: $*" >&2; exit 1; }
usage() { echo "Usage: setup.sh [--help] [--version build-<commit>]"; echo "Install daffy from the latest retained GitHub release. Set INSTALL_DIR to override ~/.local/bin."; }
version=latest
while [ "$#" -gt 0 ]; do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --version) [ "$#" -ge 2 ] || fail "--version requires a tag"; version=$2; shift 2 ;;
    *) fail "unknown option: $1" ;;
  esac
done
case "$version" in
  latest) base=https://github.com/gabriwl165/daffy/releases/latest/download ;;
  build-*) tag=${version#build-}; case "$tag" in ''|*[!0-9a-fA-F]*) fail "invalid build tag";; esac
    [ "${#tag}" -eq 40 ] || fail "invalid build tag"; base="https://github.com/gabriwl165/daffy/releases/download/$version" ;;
  *) fail "version must be latest or build-<full-commit-sha>" ;;
esac
[ "$(uname -s)" = Linux ] || fail "Linux is required"
machine=$(uname -m)
case "$machine" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) fail "unsupported architecture: $machine";; esac
command -v curl >/dev/null 2>&1 || fail "curl is required"
for cmd in tar sha256sum mktemp install mv awk wc tr; do command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is required"; done
dest=${INSTALL_DIR:-"$HOME/.local/bin"}
case "$dest" in /*) :;; *) fail "INSTALL_DIR must be an absolute path";; esac
mkdir -p "$dest" || fail "cannot create install directory: $dest"
[ ! -d "$dest/daffy" ] || fail "destination is a directory: $dest/daffy"
tmp=$(mktemp -d) || fail "cannot create temporary directory"
cleanup() { [ -z "${stage:-}" ] || rm -f "$stage"; rm -rf "$tmp"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
archive="daffy_linux_${arch}.tar.gz"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$base/$archive" -o "$tmp/$archive" || fail "download failed: $archive"
curl --fail --silent --show-error --location --proto '=https' --proto-redir '=https' "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || fail "download failed: SHA256SUMS"
expected=$(awk -v file="$archive" '$2 == file {print $1}' "$tmp/SHA256SUMS")
case "$expected" in ''|*[!0-9a-fA-F]*) fail "missing or malformed checksum for $archive";; esac
[ "${#expected}" -eq 64 ] || fail "malformed checksum for $archive"
actual=$(sha256sum "$tmp/$archive" | awk '{print $1}')
[ "$actual" = "$expected" ] || fail "checksum mismatch for $archive"
tar -tvzf "$tmp/$archive" >"$tmp/list" || fail "invalid archive"
[ "$(wc -l < "$tmp/list" | tr -d ' ')" = 1 ] || fail "archive must contain only daffy"
tar -tzf "$tmp/$archive" >"$tmp/names" || fail "invalid archive"
[ "$(awk 'NR==1 {print; exit}' "$tmp/names")" = daffy ] || fail "archive must contain only daffy"
IFS=' ' read -r kind rest <"$tmp/list"
[ "${kind#?}" != "$kind" ] && [ "${kind%${kind#?}}" = - ] && [ "${rest##* }" = daffy ] || fail "archive member must be a regular daffy file"
tar -xzf "$tmp/$archive" -C "$tmp" || fail "cannot extract archive"
[ -f "$tmp/daffy" ] && [ ! -L "$tmp/daffy" ] || fail "archive member must be a regular daffy file"
stage=$(mktemp "$dest/.daffy.XXXXXX") || fail "cannot stage installation in $dest"
install -m 0755 "$tmp/daffy" "$stage" || fail "cannot prepare installation"
mv -fT "$stage" "$dest/daffy" || fail "cannot install daffy"
stage=
echo "Installed daffy to $dest/daffy"
case ":${PATH}:" in *":$dest:"*) :;; *) echo "Add $dest to your PATH to run daffy.";; esac
