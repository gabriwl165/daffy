#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$tmp/mock" "$tmp/home with spaces" "$tmp/input" "$tmp/temp"
cat >"$tmp/mock/uname" <<'EOF'
#!/bin/sh
case "$1" in -s) echo "${MOCK_OS:-Linux}";; -m) echo "${MOCK_ARCH:-x86_64}";; esac
EOF
cat >"$tmp/mock/curl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$CURL_LOG"
case " $* " in *"--proto =https"*|*"--proto '=https'"*) :;; *) exit 90;; esac
case " $* " in *"--proto-redir =https"*|*"--proto-redir '=https'"*) :;; *) exit 91;; esac
url=; output=
while [ "$#" -gt 0 ]; do case "$1" in https://*) url=$1; shift;; -o) output=$2; shift 2;; *) shift;; esac; done
case "$url" in */daffy_linux_*.tar.gz) cp "$FIXTURE_DIR/${url##*/}" "$output";; */SHA256SUMS) cp "$FIXTURE_DIR/SHA256SUMS" "$output";; *) exit 93;; esac
[ "${FAIL_CURL:-0}" = 0 ]
EOF
chmod +x "$tmp/mock/"*
printf '#!/bin/sh\necho fixture\n' >"$tmp/input/daffy"
chmod +x "$tmp/input/daffy"
for arch in amd64 arm64; do tar -czf "$tmp/daffy_linux_$arch.tar.gz" -C "$tmp/input" daffy; done
(cd "$tmp" && sha256sum daffy_linux_*.tar.gz >SHA256SUMS)
export PATH="$tmp/mock:$PATH" FIXTURE_DIR="$tmp" CURL_LOG="$tmp/curl.log" TMPDIR="$tmp/temp"
HOME="$tmp/home with spaces" sh "$root/setup.sh" >/dev/null
[ -x "$tmp/home with spaces/.local/bin/daffy" ]
INSTALL_DIR="$tmp/custom path" sh "$root/setup.sh" --version "build-$(printf '%040d' 1)" >/dev/null
[ -x "$tmp/custom path/daffy" ]
MOCK_ARCH=aarch64 INSTALL_DIR="$tmp/arm" sh "$root/setup.sh" >/dev/null
[ -x "$tmp/arm/daffy" ]
grep -q '/releases/latest/download/daffy_linux_amd64.tar.gz' "$CURL_LOG"
grep -q '/releases/download/build-0000000000000000000000000000000000000001/daffy_linux_amd64.tar.gz' "$CURL_LOG"
grep -q 'daffy_linux_arm64.tar.gz' "$CURL_LOG"
for mode in absent malformed; do
  if [ "$mode" = absent ]; then : >"$tmp/SHA256SUMS"; else printf 'bad  daffy_linux_amd64.tar.gz\n' >"$tmp/SHA256SUMS"; fi
  if INSTALL_DIR="$tmp/$mode" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
done
(cd "$tmp" && sha256sum daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz >SHA256SUMS)
mkdir -p "$tmp/preserve"; printf old >"$tmp/preserve/daffy"
if FAIL_CURL=1 INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
[ "$(cat "$tmp/preserve/daffy")" = old ]
# A well-formed but incorrect digest, and corrupt bytes with a correct digest, both fail closed.
printf '%064d  daffy_linux_amd64.tar.gz\n' 1 >"$tmp/SHA256SUMS"
if INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
printf not-a-tar >"$tmp/daffy_linux_amd64.tar.gz"
(cd "$tmp" && sha256sum daffy_linux_amd64.tar.gz >SHA256SUMS)
if INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
[ "$(cat "$tmp/preserve/daffy")" = old ]
# Archives containing links, traversal paths, or additional members must be rejected before extraction.
for kind in symlink hardlink traversal extra; do
  mkdir -p "$tmp/$kind"
  cp "$tmp/input/daffy" "$tmp/$kind/daffy"
  case "$kind" in
    symlink) ln -sf daffy "$tmp/$kind/daffy-link"; rm "$tmp/$kind/daffy"; ln -s daffy-link "$tmp/$kind/daffy"; tar -czf "$tmp/daffy_linux_amd64.tar.gz" -C "$tmp/$kind" daffy ;;
    hardlink) ln -f "$tmp/$kind/daffy" "$tmp/$kind/link"; tar -czf "$tmp/daffy_linux_amd64.tar.gz" -C "$tmp/$kind" daffy link ;;
    traversal) tar -czf "$tmp/daffy_linux_amd64.tar.gz" -C "$tmp/$kind" --transform='s,daffy,../daffy,' daffy ;;
    extra) printf extra >"$tmp/$kind/extra"; tar -czf "$tmp/daffy_linux_amd64.tar.gz" -C "$tmp/$kind" daffy extra ;;
  esac
  (cd "$tmp" && sha256sum daffy_linux_amd64.tar.gz >SHA256SUMS)
  if INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
  [ "$(cat "$tmp/preserve/daffy")" = old ]
done
if MOCK_OS=Darwin sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
if MOCK_ARCH=mips INSTALL_DIR="$tmp/no" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
if INSTALL_DIR=relative sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
if sh "$root/setup.sh" --version build-../../etc >/dev/null 2>&1; then exit 1; fi
[ -z "$(find "$tmp" -name '.daffy.*' -print -quit)" ]
[ -z "$(find "$tmp/temp" -mindepth 1 -print -quit)" ]
