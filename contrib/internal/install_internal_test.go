// Tests for install-internal.sh, the install side of an internal build. Each
// test copies the script into a checkout of its own, with the key at the
// fixed place (docs/internal-builds.pub) that the script reads, and runs it
// under a HOME and a PATH of the test's own. The PATH holds the tools the
// script uses and nothing else: a uname stand-in says which system it is on,
// so the tests give the same answers on a Mac and on Linux, and a missing
// minisign is a missing minisign. The keys are throwaway, made here with the
// real minisign, which the tests need: a signature check against a stand-in
// would prove nothing.
package internal

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const testCommit = "abcdef0123456789abcdef0123456789abcdef01"

// fakeYardr is a yardr that says it was built from testCommit. The text after
// the version is what "yardr version" prints, with the 12-character commit in
// parentheses as the real one does.
func fakeYardr(commit string) string {
	return "#!/bin/sh\nif [ \"$1\" = version ]; then echo \"v9.8.7-3-gabcdef0 (" + commit[:12] + ")\"; fi\n"
}

// rig is one test's checkout, home and tools.
type rig struct {
	t       *testing.T
	root    string // a checkout: contrib/internal/install-internal.sh and docs/
	home    string
	tools   string // the PATH the script runs under
	release string // the directory a release was downloaded to
	os      string // what the uname stand-in says
	arch    string
}

// newRig makes a checkout with the script in it, and a PATH with sh and the
// tools it uses. minisign is linked in only when withMinisign is set.
func newRig(t *testing.T, osName, arch string, withMinisign bool) *rig {
	t.Helper()
	r := &rig{t: t, os: osName, arch: arch}
	r.root = t.TempDir()
	r.home = t.TempDir()
	r.tools = t.TempDir()
	r.release = t.TempDir()

	script, err := os.ReadFile("install-internal.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(r.root, "contrib", "internal")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install-internal.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"sh", "awk", "cat", "cp", "cut", "chmod", "mkdir", "mktemp", "mv", "rm", "sha256sum", "shasum", "printf"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		if err := os.Symlink(p, filepath.Join(r.tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	if withMinisign {
		p, err := exec.LookPath("minisign")
		if err != nil {
			t.Fatalf("these tests sign with the real minisign, and it is not on PATH: %v (brew install minisign, or apt install minisign)", err)
		}
		if err := os.Symlink(p, filepath.Join(r.tools, "minisign")); err != nil {
			t.Fatal(err)
		}
	}
	uname := "#!/bin/sh\ncase $1 in -s) echo " + osName + " ;; -m) echo " + arch + " ;; esac\n"
	if err := os.WriteFile(filepath.Join(r.tools, "uname"), []byte(uname), 0o755); err != nil {
		t.Fatal(err)
	}
	return r
}

// keyPair makes a throwaway minisign key pair in dir, with no password, and
// returns the public key's path and the secret key's.
func (r *rig) keyPair(dir string) (pub, sec string) {
	r.t.Helper()
	pub = filepath.Join(dir, "k.pub")
	sec = filepath.Join(dir, "k.key")
	out, err := exec.Command("minisign", "-G", "-W", "-p", pub, "-s", sec).CombinedOutput()
	if err != nil {
		r.t.Fatalf("minisign -G: %v\n%s", err, out)
	}
	return pub, sec
}

// commitKey puts the public key at the fixed place in the checkout, the place
// the script reads it from.
func (r *rig) commitKey(pub string) {
	r.t.Helper()
	data, err := os.ReadFile(pub)
	if err != nil {
		r.t.Fatal(err)
	}
	dir := filepath.Join(r.root, "docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal-builds.pub"), data, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// sha is the sha256 of data, hex, as SHA256SUMS holds it.
func sha(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// writeRelease writes the two binaries and SHA256SUMS into the release
// directory. The sums name each binary by its sha, unless sumsOverride is set.
func (r *rig) writeRelease(sumsOverride string) {
	r.t.Helper()
	linux := []byte(fakeYardr(testCommit))
	darwin := []byte(fakeYardr(testCommit) + "# darwin\n")
	if err := os.WriteFile(filepath.Join(r.release, "yardr_linux_amd64"), linux, 0o644); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.release, "yardr_darwin_arm64"), darwin, 0o644); err != nil {
		r.t.Fatal(err)
	}
	sums := sha(linux) + "  yardr_linux_amd64\n" + sha(darwin) + "  yardr_darwin_arm64\n"
	if sumsOverride != "" {
		sums = sumsOverride
	}
	if err := os.WriteFile(filepath.Join(r.release, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// sign signs the release's SHA256SUMS with sec, into SHA256SUMS.minisig.
func (r *rig) sign(sec string) {
	r.t.Helper()
	out, err := exec.Command("minisign", "-S", "-W", "-s", sec,
		"-m", filepath.Join(r.release, "SHA256SUMS"),
		"-x", filepath.Join(r.release, "SHA256SUMS.minisig")).CombinedOutput()
	if err != nil {
		r.t.Fatalf("minisign -S: %v\n%s", err, out)
	}
}

// run runs the script with args and returns its output and error.
func (r *rig) run(args ...string) (string, error) {
	cmd := exec.Command("sh", append([]string{filepath.Join(r.root, "contrib", "internal", "install-internal.sh")}, args...)...)
	cmd.Env = []string{"PATH=" + r.tools, "HOME=" + r.home, "TMPDIR=" + r.home}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// ok runs the script and fails the test unless it exits 0.
func (r *rig) ok(args ...string) string {
	r.t.Helper()
	out, err := r.run(args...)
	if err != nil {
		r.t.Fatalf("install-internal.sh %v: %v\n%s", args, err, out)
	}
	return out
}

// refused runs the script and fails the test unless it exits non-zero with
// want in its output, and nothing was installed in the home.
func (r *rig) refused(want string, args ...string) {
	r.t.Helper()
	out, err := r.run(args...)
	if err == nil {
		r.t.Fatalf("install-internal.sh %v succeeded, want a refusal:\n%s", args, out)
	}
	if !strings.Contains(out, want) {
		r.t.Errorf("refusal says %q, want it to contain %q", out, want)
	}
	if _, statErr := os.Stat(filepath.Join(r.home, ".local", "bin", "yardr")); statErr == nil {
		r.t.Errorf("a refused install left a yardr in ~/.local/bin")
	}
}

// signedRelease is a release signed with a key the checkout trusts.
func signedRelease(t *testing.T, osName, arch string) *rig {
	r := newRig(t, osName, arch, true)
	keys := t.TempDir()
	pub, sec := r.keyPair(keys)
	r.commitKey(pub)
	r.writeRelease("")
	r.sign(sec)
	return r
}

func TestInstallInternalPutsTheBuildInLocalBin(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	out := r.ok(r.release, testCommit)
	if !strings.Contains(out, "SHA256SUMS verifies against docs/internal-builds.pub") {
		t.Errorf("the output does not say the signature was checked:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(r.home, ".local", "bin", "yardr"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(r.release, "yardr_linux_amd64"))
	if !bytes.Equal(got, want) {
		t.Errorf("the installed yardr is not the release's linux binary")
	}
	info, err := os.Stat(filepath.Join(r.home, ".local", "bin", "yardr"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the installed yardr is not executable: %v", info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Join(r.home, ".local", "bin"))
	for _, e := range entries {
		if e.Name() != "yardr" {
			t.Errorf("a temporary file was left in ~/.local/bin: %s", e.Name())
		}
	}
}

func TestInstallInternalDarwinVerifiesAndLeavesTheBinaryToTheYard(t *testing.T) {
	r := signedRelease(t, "Darwin", "arm64")
	out := r.ok(r.release, testCommit)
	if !strings.Contains(out, "yard-qxbr") {
		t.Errorf("the darwin output does not hand codesign and the pin to the yard (yard-qxbr):\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(r.home, ".local", "bin", "yardr")); err == nil {
		t.Errorf("a darwin verify installed a yardr; the swap is the yard's step")
	}
	if _, err := os.Stat(filepath.Join(r.release, "yardr_darwin_arm64")); err != nil {
		t.Errorf("the verified darwin binary is not left in the release directory: %v", err)
	}
}

// A release whose sums were changed after signing, here a fresh sum for a
// different binary, is refused: the signature covers SHA256SUMS, so an
// attacker who swaps a binary cannot also swap its line.
func TestInstallInternalRefusesTamperedSha256Sums(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	evil := []byte(fakeYardr(testCommit) + "# evil\n")
	if err := os.WriteFile(filepath.Join(r.release, "yardr_linux_amd64"), evil, 0o644); err != nil {
		t.Fatal(err)
	}
	sums := sha(evil) + "  yardr_linux_amd64\n"
	if err := os.WriteFile(filepath.Join(r.release, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	r.refused("SHA256SUMS does not verify against docs/internal-builds.pub", r.release, testCommit)
}

// The same refusal for a sum edited in place: one character of a line.
func TestInstallInternalRefusesAOneCharacterSumsEdit(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	data, err := os.ReadFile(filepath.Join(r.release, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	edited := []byte(strings.Replace(string(data), "  yardr_linux_amd64", "  yardr_linux_amd64 ", 1))
	if err := os.WriteFile(filepath.Join(r.release, "SHA256SUMS"), edited, 0o644); err != nil {
		t.Fatal(err)
	}
	r.refused("SHA256SUMS does not verify", r.release, testCommit)
}

// A signature by a key the checkout does not carry is no signature for it.
func TestInstallInternalRefusesASignatureFromAnotherKey(t *testing.T) {
	r := newRig(t, "Linux", "x86_64", true)
	pub, _ := r.keyPair(t.TempDir())
	r.commitKey(pub)
	_, otherSec := r.keyPair(t.TempDir())
	r.writeRelease("")
	r.sign(otherSec)
	r.refused("does not verify against docs/internal-builds.pub", r.release, testCommit)
}

// The signed sums are right, and a binary was swapped under them: the
// binary's own sha is what it is checked against.
func TestInstallInternalRefusesABinaryThatDoesNotMatchItsSum(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	evil := []byte(fakeYardr(testCommit) + "# swapped after the release\n")
	if err := os.WriteFile(filepath.Join(r.release, "yardr_linux_amd64"), evil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.refused("does not match its sha256 in SHA256SUMS", r.release, testCommit)
}

// A binary that says another commit is refused: the release is for the commit
// asked, and no other.
func TestInstallInternalRefusesAnotherCommit(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	r.refused("does not name the commit 111111111111", r.release, "1111111111111111111111111111111111111111")
}

func TestInstallInternalRefusesWithoutMinisign(t *testing.T) {
	r := newRig(t, "Linux", "x86_64", false)
	keys := t.TempDir()
	pub, sec := r.keyPair(keys)
	r.commitKey(pub)
	r.writeRelease("")
	r.sign(sec)
	r.refused("minisign is not on PATH", r.release, testCommit)
}

func TestInstallInternalRefusesWithoutTheCommittedKey(t *testing.T) {
	r := newRig(t, "Linux", "x86_64", true)
	keys := t.TempDir()
	_, sec := r.keyPair(keys)
	r.writeRelease("")
	r.sign(sec)
	r.refused("docs/internal-builds.pub is not committed", r.release, testCommit)
}

// The key is read from the checkout the script sits in, never from the
// release: a release that carries its own key and signs with it is still
// refused.
func TestInstallInternalIgnoresAKeyInTheRelease(t *testing.T) {
	r := newRig(t, "Linux", "x86_64", true)
	keys := t.TempDir()
	pub, _ := r.keyPair(keys)
	r.commitKey(pub)
	evilPub, evilSec := r.keyPair(t.TempDir())
	data, err := os.ReadFile(evilPub)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.release, "internal-builds.pub"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	r.writeRelease("")
	r.sign(evilSec)
	r.refused("does not verify against docs/internal-builds.pub", r.release, testCommit)
}

func TestInstallInternalUsage(t *testing.T) {
	r := newRig(t, "Linux", "x86_64", true)
	for _, args := range [][]string{{}, {r.release}, {r.release, testCommit, "extra"}} {
		out, err := r.run(args...)
		if err == nil || !strings.Contains(out, "usage: install-internal.sh") {
			t.Errorf("args %q: want the usage line and a failure, got %v: %s", args, err, out)
		}
	}
}

func TestInstallInternalRefusesAShortCommit(t *testing.T) {
	r := signedRelease(t, "Linux", "x86_64")
	r.refused("the commit must be at least 12 hex digits", r.release, "abcd")
}
