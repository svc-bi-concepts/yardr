# contrib/internal

`install-internal.sh` installs an internal build of yardr: the binary CI builds
from a commit on `main` and publishes as a prerelease `internal-<short sha>` on
the private mirror. It is the install side of that build, and it is separate
from `install.sh`, which installs public releases against yardr's release key.
`install.sh` and its tests are unchanged by it.

    contrib/internal/install-internal.sh <release-dir> <commit>

`<release-dir>` holds `SHA256SUMS`, `SHA256SUMS.minisig` and this system's
binary (`yardr_linux_amd64` or `yardr_darwin_arm64`), downloaded from the
release. `<commit>` is the full sha the release was built from.

## What it checks, in order

1. `minisign` is on PATH. Without it nothing is installed.
2. `minisign -V` checks `SHA256SUMS` against `docs/internal-builds.pub`, the key
   at a fixed place in this checkout. The release never supplies the key.
3. The binary's sha256 matches its line in the verified `SHA256SUMS`.
4. The binary, run from a copy, says the commit: `yardr version` names the
   first 12 characters of `<commit>`. Only this system's binary is run.
5. Linux: the binary goes to `~/.local/bin/yardr` through a temporary file and
   a rename, so a running yardr is replaced whole.

Any failure stops the install and writes nothing to `~/.local/bin`.

## darwin (the mini)

On darwin steps 1 to 4 run, and then the script stops. The verified binary
stays in `<release-dir>`. Codesigning, the pin of the signed sha (yard-qxbr)
and the swap are the yard's own step, and this script does none of them, so a
darwin install is not complete from it alone.

## The key

The public key is `docs/internal-builds.pub`. It is not committed yet, and
until it is, every internal install refuses with a message that names the file.
This is deliberate: a missing key fails closed.

**One-time key step (the yard's admin makes it; a builder does not).** The admin
generates the BI Concepts internal-build key pair, keeps the secret key only in
the secret store the signing job reads it from, and commits the public key as
`docs/internal-builds.pub` in one change. A key rotation is the same change in
the same commit: a new public key replaces the old one, and the signing side
moves to the new secret in the same step, so no release is ever checked
against a key that does not sign it.

Keep the key apart from the release key. `install.sh` checks public releases
against yardr's release key; this script checks only against the internal-build
key.

## Fetching a release

With the GitHub CLI and access to the mirror:

    gh release download internal-<short sha> --repo <mirror> --dir <release-dir>

The mirror's name is the operator's to set; no name is written here.

## Tests

`install_internal_test.go` runs the script in a throwaway checkout, with a
throwaway key pair made by the real `minisign`, which must be on PATH. It
covers a good install on Linux, a darwin verify that leaves the swap to the
yard, a tampered `SHA256SUMS`, a one-character edit to it, a signature from
another key, a binary that does not match its sum, a binary that names another
commit, no minisign, no committed key, a key carried in the release, and the
usage and the short commit.
