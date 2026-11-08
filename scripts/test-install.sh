#!/bin/sh
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
host_os=$(uname -s)
host_arch=$(uname -m)
REAL_UNAME=$(command -v uname)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
hash_files() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@";
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$@";
  else echo "sha256sum or shasum is required" >&2; return 1; fi
}
mkdir -p "$tmp/mock" "$tmp/home with spaces" "$tmp/input" "$tmp/temp"
mkdir -p "$tmp/home with spaces/.config"
printf 'export TEST_PROFILE=untouched\n' >"$tmp/home with spaces/.profile"
printf 'export TEST_RC=untouched\n' >"$tmp/home with spaces/.bashrc"
cp "$tmp/home with spaces/.profile" "$tmp/profile.expected"
cp "$tmp/home with spaces/.bashrc" "$tmp/bashrc.expected"
cat >"$tmp/mock/hostile-editor" <<'EOF'
#!/bin/sh
printf '%s\n' "$0 $*" >>"$EDITOR_LOG"
exit 99
EOF
chmod +x "$tmp/mock/hostile-editor"
cat >"$tmp/mock/uname" <<'EOF'
#!/bin/sh
case "$1" in -s) echo "${MOCK_OS:-$("$REAL_UNAME" -s)}";; -m) echo "${MOCK_ARCH:-$("$REAL_UNAME" -m)}";; esac
EOF
cat >"$tmp/mock/curl" <<'EOF'
#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$CURL_LOG"
case " $* " in *"--proto =https"*|*"--proto '=https'"*) :;; *) exit 90;; esac
case " $* " in *"--proto-redir =https"*|*"--proto-redir '=https'"*) :;; *) exit 91;; esac
url=; output=
while [ "$#" -gt 0 ]; do case "$1" in https://*) url=$1; shift;; -o) output=$2; shift 2;; *) shift;; esac; done
case "$url" in */daffy_linux_*.tar.gz|*/daffy_darwin_*.tar.gz) cp "$FIXTURE_DIR/${url##*/}" "$output";; */SHA256SUMS) cp "$FIXTURE_DIR/SHA256SUMS" "$output";; *) exit 93;; esac
[ "${FAIL_CURL:-0}" = 0 ]
EOF
cat >"$tmp/mock/mv" <<'EOF'
#!/bin/sh
case "$1" in
  -fh) shift; if [ "$REAL_HOST_OS" = Darwin ]; then exec /bin/mv -fh "$@"; else exec /bin/mv -f "$@"; fi;;
  -fT) if [ "$REAL_HOST_OS" = Darwin ]; then shift; exec /bin/mv -fh "$@"; else exec /bin/mv "$@"; fi;;
  *) exec /bin/mv "$@";;
esac
EOF
if ! command -v sha256sum >/dev/null 2>&1; then
  printf '#!/bin/sh\nexec shasum -a 256 "$@"\n' >"$tmp/mock/sha256sum"
fi
chmod +x "$tmp/mock/"*
printf '#!/bin/sh\necho fixture-one\n' >"$tmp/input/daffy"
chmod +x "$tmp/input/daffy"
for archive in daffy_linux_amd64 daffy_linux_arm64 daffy_darwin_amd64 daffy_darwin_arm64; do tar -czf "$tmp/$archive.tar.gz" -C "$tmp/input" daffy; done
(cd "$tmp" && hash_files daffy_linux_*.tar.gz daffy_darwin_*.tar.gz >SHA256SUMS)
export FIXTURE_DIR="$tmp" CURL_LOG="$tmp/curl.log" TMPDIR="$tmp/temp" REAL_UNAME REAL_HOST_OS="$host_os" EDITOR_LOG="$tmp/editor.log"
export EDITOR="$tmp/mock/hostile-editor" VISUAL="$tmp/mock/hostile-editor" PAGER="$tmp/mock/hostile-editor"
export PATH="$tmp/mock:$PATH"
# First exercise the actual host uname, checksum utility, and mv implementation.
case "$host_os:$host_arch" in
  Linux:x86_64|Linux:amd64) native_os=Linux; native_arch=amd64; native_platform=linux;;
  Linux:aarch64|Linux:arm64) native_os=Linux; native_arch=arm64; native_platform=linux;;
  Darwin:arm64|Darwin:aarch64) native_os=Darwin; native_arch=arm64; native_platform=darwin;;
  Darwin:x86_64|Darwin:amd64) native_os=Darwin; native_arch=amd64; native_platform=darwin;;
  *) echo "unsupported test host: $host_os/$host_arch" >&2; exit 1;;
esac
HOME="$tmp/home with spaces" sh "$root/setup.sh" </dev/null >"$tmp/install.log"
[ -x "$tmp/home with spaces/.local/bin/daffy" ]
"$tmp/home with spaces/.local/bin/daffy" | grep -q fixture-one
grep -q 'Downloading daffy release archive' "$tmp/install.log"
grep -q 'Verifying release checksum' "$tmp/install.log"
printf '#!/bin/sh\necho fixture-two\n' >"$tmp/input/daffy"
chmod +x "$tmp/input/daffy"
for archive in daffy_linux_amd64 daffy_linux_arm64 daffy_darwin_amd64 daffy_darwin_arm64; do tar -czf "$tmp/$archive.tar.gz" -C "$tmp/input" daffy; done
(cd "$tmp" && hash_files daffy_linux_*.tar.gz daffy_darwin_*.tar.gz >SHA256SUMS)
HOME="$tmp/home with spaces" sh "$root/setup.sh" </dev/null >"$tmp/update.log"
"$tmp/home with spaces/.local/bin/daffy" | grep -q fixture-two
[ ! -e "$EDITOR_LOG" ]
cmp "$tmp/profile.expected" "$tmp/home with spaces/.profile"
cmp "$tmp/bashrc.expected" "$tmp/home with spaces/.bashrc"
grep -q "${native_platform}_${native_arch}.tar.gz" "$CURL_LOG"
MOCK_OS=Linux MOCK_ARCH=x86_64 HOME="$tmp/home with spaces" sh "$root/setup.sh" >/dev/null
MOCK_OS=Linux MOCK_ARCH=x86_64 INSTALL_DIR="$tmp/custom path" sh "$root/setup.sh" --version "build-$(printf '%040d' 1)" >/dev/null
[ -x "$tmp/custom path/daffy" ]
MOCK_OS=Linux MOCK_ARCH=aarch64 INSTALL_DIR="$tmp/arm" sh "$root/setup.sh" >/dev/null
[ -x "$tmp/arm/daffy" ]
grep -q '/releases/latest/download/daffy_linux_amd64.tar.gz' "$CURL_LOG"
grep -q '/releases/download/build-0000000000000000000000000000000000000001/daffy_linux_amd64.tar.gz' "$CURL_LOG"
grep -q 'daffy_linux_arm64.tar.gz' "$CURL_LOG"
MOCK_OS=Darwin MOCK_ARCH=arm64 INSTALL_DIR="$tmp/mac" sh "$root/setup.sh" >/dev/null
[ -x "$tmp/mac/daffy" ]
grep -q 'daffy_darwin_arm64.tar.gz' "$CURL_LOG"
MOCK_OS=Darwin MOCK_ARCH=x86_64 INSTALL_DIR="$tmp/mac-intel" sh "$root/setup.sh" >/dev/null
[ -x "$tmp/mac-intel/daffy" ]
grep -q 'daffy_darwin_amd64.tar.gz' "$CURL_LOG"
mkdir -p "$tmp/preserve-dir/daffy" "$tmp/target-dir" "$tmp/symlink-dir"
printf old >"$tmp/target-dir/keep"
ln -s "$tmp/target-dir" "$tmp/symlink-dir/daffy"
for bad_dest in "$tmp/preserve-dir" "$tmp/symlink-dir"; do
  if MOCK_OS=Darwin MOCK_ARCH=arm64 INSTALL_DIR="$bad_dest" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
done
if [ "$(cat "$tmp/target-dir/keep")" != old ]; then exit 1; fi
for mode in absent malformed; do
  if [ "$mode" = absent ]; then : >"$tmp/SHA256SUMS"; else printf 'bad  daffy_linux_amd64.tar.gz\n' >"$tmp/SHA256SUMS"; fi
  if INSTALL_DIR="$tmp/$mode" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
done
(cd "$tmp" && hash_files daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz daffy_darwin_arm64.tar.gz >SHA256SUMS)
mkdir -p "$tmp/preserve"; printf old >"$tmp/preserve/daffy"
if FAIL_CURL=1 INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
[ "$(cat "$tmp/preserve/daffy")" = old ]
# Darwin checksum and archive failures must also preserve an existing installation.
: >"$tmp/SHA256SUMS"
if MOCK_OS=Darwin MOCK_ARCH=arm64 INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
(cd "$tmp" && hash_files daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz daffy_darwin_arm64.tar.gz >SHA256SUMS)
printf invalid >"$tmp/daffy_darwin_arm64.tar.gz"
(cd "$tmp" && hash_files daffy_darwin_arm64.tar.gz >SHA256SUMS)
if MOCK_OS=Darwin MOCK_ARCH=arm64 INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
[ "$(cat "$tmp/preserve/daffy")" = old ]
(cd "$tmp" && hash_files daffy_linux_amd64.tar.gz daffy_linux_arm64.tar.gz daffy_darwin_arm64.tar.gz >SHA256SUMS)
# A well-formed but incorrect digest, and corrupt bytes with a correct digest, both fail closed.
printf '%064d  daffy_linux_amd64.tar.gz\n' 1 >"$tmp/SHA256SUMS"
if INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
printf not-a-tar >"$tmp/daffy_linux_amd64.tar.gz"
(cd "$tmp" && hash_files daffy_linux_amd64.tar.gz >SHA256SUMS)
if INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
[ "$(cat "$tmp/preserve/daffy")" = old ]
for artifact in daffy_linux_amd64 daffy_darwin_amd64 daffy_darwin_arm64; do
  for kind in symlink hardlink traversal extra; do
    python3 - "$tmp/$artifact.tar.gz" "$kind" <<'PY'
import io, sys, tarfile
path, kind = sys.argv[1:]
with tarfile.open(path, 'w:gz') as archive:
    def member(name, typ=tarfile.REGTYPE, data=b'fixture', link=''):
        info = tarfile.TarInfo(name)
        info.type = typ
        info.linkname = link
        info.size = len(data) if typ == tarfile.REGTYPE else 0
        archive.addfile(info, io.BytesIO(data) if info.size else None)
    if kind == 'symlink': member('daffy', tarfile.SYMTYPE, link='outside')
    elif kind == 'hardlink': member('daffy'); member('linked', tarfile.LNKTYPE, link='daffy')
    elif kind == 'traversal': member('../daffy')
    else: member('daffy'); member('extra')
PY
    (cd "$tmp" && hash_files "$artifact.tar.gz" >SHA256SUMS)
    case "$artifact" in daffy_darwin_amd64) test_os=Darwin; test_arch=x86_64;; daffy_darwin_arm64) test_os=Darwin; test_arch=arm64;; *) test_os=Linux; test_arch=x86_64;; esac
    if MOCK_OS="$test_os" MOCK_ARCH="$test_arch" INSTALL_DIR="$tmp/preserve" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
    [ "$(cat "$tmp/preserve/daffy")" = old ]
  done
done
if MOCK_ARCH=mips INSTALL_DIR="$tmp/no" sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
if INSTALL_DIR=relative sh "$root/setup.sh" >/dev/null 2>&1; then exit 1; fi
if sh "$root/setup.sh" --version build-../../etc >/dev/null 2>&1; then exit 1; fi
[ -z "$(find "$tmp" -name '.daffy.*' -print -quit)" ]
[ -z "$(find "$tmp/temp" -mindepth 1 -print -quit)" ]
