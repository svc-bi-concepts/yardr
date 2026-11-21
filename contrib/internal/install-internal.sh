#!/bin/sh
# install-internal.sh: installs an internal build of yardr, the prerelease CI
# publishes on the private mirror for each commit on main (internal-<short
# sha>). It is the install side of that build and not install.sh's: a public
# release is checked against yardr's release key, and an internal build against
# the BI Concepts key committed at docs/internal-builds.pub, which only this
# script and the key's own step ever read. install.sh is unchanged by it.
#
#   contrib/internal/install-internal.sh <release-dir> <commit>
#
# <release-dir> holds what the release was downloaded to: SHA256SUMS,
# SHA256SUMS.minisig and this system's binary (yardr_linux_amd64 or
# yardr_darwin_arm64). <commit> is the full sha the release was built from.
#
# The checks run in this order, and a failure stops the install with nothing
# written to ~/.local/bin:
#
#   1. minisign is on PATH: without it nothing is trusted, so it is refused.
#   2. minisign -V checks SHA256SUMS against docs/internal-builds.pub, the key
#      at a fixed place in this checkout, never one the release carries.
#   3. The binary's sha256 is its line in SHA256SUMS.
#   4. The binary, run here, says the commit it was built from ("yardr version"
#      names the 12-character sha). This runs only for this system's binary.
#   5. Linux: the binary is put into ~/.local/bin/yardr, through a temporary
#      file and a rename, as install.sh does.
#
# darwin (the mini): steps 1 to 4 run, and the verified binary is left where it
# is, in <release-dir>, for the yard's own step: codesign, the pin of the signed
# sha (yard-qxbr), and the swap. This script does none of those on darwin, so
# a darwin install is not complete from it alone.
#
# POSIX sh. Needs minisign, awk, and sha256sum or shasum. The script is
# functions and its last line calls main, so a cut-short copy runs nothing.
set -eu
LC_ALL=C
export LC_ALL

work=
partial=
cleanup() {
  [ -z "$work" ] || rm -rf "$work"
  [ -z "$partial" ] || rm -f "$partial"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

say() { printf 'install-internal.sh: %s\n' "$*"; }
die() {
  printf 'install-internal.sh: %s\n' "$*" >&2
  exit 1
}

usage() {
  echo "usage: install-internal.sh <release-dir> <commit>" >&2
  echo "       (on darwin, codesign, the pin and the swap are the yard's step: yard-qxbr)" >&2
  exit 2
}

# sha256_of <file>: its sha256, from sha256sum where there is one and from
# shasum where not (macOS), as install.sh does.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ print $1 }'
  else
    shasum -a 256 "$1" | awk '{ print $1 }'
  fi
}

main() {
  [ $# -eq 2 ] || usage
  dir=$1
  commit=$2

  command -v minisign >/dev/null 2>&1 ||
    die "minisign is not on PATH; an internal build is not installed without it (brew install minisign on macOS, apt install minisign on Debian)"
  command -v awk >/dev/null 2>&1 || die "awk is not on PATH, and the install needs it"
  command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 ||
    die "neither sha256sum nor shasum is on PATH, so the binary could not be checked"
  [ -n "${HOME:-}" ] || die "HOME is not set"

  case $commit in
  "" | *[!0-9a-f]*) die "the commit must be a full lowercase sha, got '$commit'" ;;
  esac
  [ "${#commit}" -ge 12 ] || die "the commit must be at least 12 hex digits, got '$commit'"
  short=$(printf '%s' "$commit" | cut -c1-12)

  # The key is found beside the script, in this checkout: contrib/internal/ is
  # two levels under the root. A copy of the script that is moved out of the
  # checkout finds no key and refuses, rather than look for one elsewhere.
  case $0 in
  */*) here=${0%/*} ;;
  *) here=. ;;
  esac
  root=$(cd "$here/../.." && pwd) || die "cannot read the checkout the script is in"
  pubkey=$root/docs/internal-builds.pub
  [ -f "$pubkey" ] ||
    die "$pubkey is not committed, so no internal build can be checked; the key is made once by the yard's admin (contrib/internal/README.md, the key step), and nothing was installed"

  case $(uname -s) in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "there is no internal build for $(uname -s)" ;;
  esac
  case $(uname -m) in
  arm64 | aarch64) arch=arm64 ;;
  x86_64 | amd64) arch=amd64 ;;
  *) die "there is no internal build for $(uname -m)" ;;
  esac
  # Built for the two targets the VM and the mini run; any other pair has no
  # build to check.
  case $os-$arch in
  linux-amd64 | darwin-arm64) ;;
  *) die "there is no internal build for $os $arch; the builds are linux amd64 and darwin arm64" ;;
  esac
  bin=yardr_${os}_$arch

  [ -f "$dir/SHA256SUMS" ] && [ -f "$dir/SHA256SUMS.minisig" ] && [ -f "$dir/$bin" ] ||
    die "$dir does not hold SHA256SUMS, SHA256SUMS.minisig and $bin; nothing was installed"

  # 2. The signature on the sums, against the committed key. minisign's own
  # words on a bad signature go to stderr, which is left open.
  minisign -V -p "$pubkey" -x "$dir/SHA256SUMS.minisig" -m "$dir/SHA256SUMS" >/dev/null ||
    die "SHA256SUMS does not verify against docs/internal-builds.pub; nothing was installed"
  say "SHA256SUMS verifies against docs/internal-builds.pub"

  # 3. The binary's sha256 against its line in the signed sums.
  want=$(awk -v name="$bin" '$2 == name || $2 == "*" name { print $1; exit }' "$dir/SHA256SUMS")
  case $want in
  "" | *[!0-9a-f]*) die "SHA256SUMS has no sha256 for $bin; nothing was installed" ;;
  esac
  [ "${#want}" -eq 64 ] || die "SHA256SUMS has no sha256 for $bin; nothing was installed"
  [ "$(sha256_of "$dir/$bin")" = "$want" ] ||
    die "$bin does not match its sha256 in SHA256SUMS; nothing was installed"
  say "$bin matches its sha256 in SHA256SUMS"

  # 4. The binary says which commit it was built from. It is run from a copy,
  # so the release directory need not be executable and nothing runs from it.
  work=$(mktemp -d "${TMPDIR:-/tmp}/yardr-internal.XXXXXX") ||
    die "cannot make a temporary directory in ${TMPDIR:-/tmp}"
  cp "$dir/$bin" "$work/yardr"
  chmod 755 "$work/yardr"
  got=$("$work/yardr" version </dev/null 2>&1) ||
    die "the yardr in $bin does not run here (it said: $got); nothing was installed"
  case $got in
  *"($short"*) ;;
  *) die "yardr version prints \"$got\", which does not name the commit $short; nothing was installed" ;;
  esac
  say "yardr version names the commit $short: $got"

  if [ "$os" = darwin ]; then
    say "$dir/$bin is verified; codesign, the pin of the signed sha and the swap are the yard's step (yard-qxbr), not this script's. Nothing was installed."
    exit 0
  fi

  # 5. Linux: into ~/.local/bin the way install.sh puts a release there.
  bindir=$HOME/.local/bin
  mkdir -p "$bindir" || die "cannot make $bindir"
  [ ! -d "$bindir/yardr" ] || die "$bindir/yardr is a directory; nothing was installed"
  partial=$(mktemp "$bindir/.yardr.XXXXXX") || die "cannot write in $bindir"
  cp "$work/yardr" "$partial"
  chmod 755 "$partial"
  mv -f "$partial" "$bindir/yardr"
  partial=
  say "installed $bindir/yardr from the internal build of $short"
}

main "$@"
