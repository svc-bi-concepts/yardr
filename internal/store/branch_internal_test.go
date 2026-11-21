package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// publicBranch names a bug fix and anything else feat, from the title's words
// in lower case; a name already taken gets the next number.
func TestPublicBranchNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ typ, title, want string }{
		{"bug", "Login fails on Safari!", "fix/login-fails-on-safari"},
		{"task", "Add  a -- feature", "feat/add-a-feature"},
		{"feature", "", "feat/bead"},
		{"task", "  ünïcode only  ", "feat/n-code-only"},
		{"task", "100% done", "feat/100-done"},
	} {
		if got := publicBranch(tc.typ, tc.title, func(string) bool { return false }); got != tc.want {
			t.Errorf("publicBranch(%q, %q) = %q, want %q", tc.typ, tc.title, got, tc.want)
		}
	}
	long := publicBranch("task", strings.Repeat("a", 60)+" tail", func(string) bool { return false })
	if slug := strings.TrimPrefix(long, "feat/"); len(slug) > branchSlugMax || strings.HasSuffix(slug, "-") || slug == "" {
		t.Errorf("a long title's slug = %q, want at most %d bytes and no dash at its end", slug, branchSlugMax)
	}
	taken := map[string]bool{"fix/x": true, "fix/x-2": true}
	if got := publicBranch("bug", "X", func(n string) bool { return taken[n] }); got != "fix/x-3" {
		t.Errorf("a third name for one title = %q, want fix/x-3", got)
	}
}

// The public names are the shape IsBeadBranch accepts, and a returned branch
// is held to that shape: the names a branch can take from a title pass, and a
// name that is not one of them does not.
func TestPublicBranchShapeIsAcceptedBack(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fix/login-fails-on-safari", "feat/n-code-only", "fix/x-3", "feat/bead"} {
		if !IsBeadBranch(name) {
			t.Errorf("IsBeadBranch(%q) = false for a name a title gives", name)
		}
	}
	for _, name := range []string{"fix/", "feat/-x", "fix/Upper", "fix/a..b", "fix/a/b", "release/x", "fix/a--b", "fix/a-"} {
		if IsBeadBranch(name) {
			t.Errorf("IsBeadBranch(%q) = true for a name no title gives", name)
		}
	}
}

// branchSlug keeps the ASCII letters and digits of a title, one dash between
// runs of anything else, and "bead" for a title with none.
func TestBranchSlug(t *testing.T) {
	t.Parallel()
	for title, want := range map[string]string{
		"Fix: the thing (now)": "fix-the-thing-now",
		"__":                   "bead",
		"":                     "bead",
		"a":                    "a",
	} {
		if got := branchSlug(title); got != want {
			t.Errorf("branchSlug(%q) = %q, want %q", title, got, want)
		}
	}
}

// pushBranch pushes the branch it is given under that name, so a branch whose
// name is not yard/<handle> reaches the peer as the name the bead records.
func TestPushBranchPushesTheNameItIsGiven(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	repo := filepath.Join(dir, "repo")
	pushGit(t, dir, "init", "-q", "--bare", remote)
	pushGit(t, dir, "init", "-q", repo)
	pushGit(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	pushGit(t, repo, "branch", "fix/login-fails-on-safari")
	pushGit(t, repo, "remote", "add", "origin", remote)

	if err := pushBranch(context.Background(), Depot{Name: "web", Path: repo}, "fix/login-fails-on-safari", "origin"); err != nil {
		t.Fatalf("pushBranch: %v", err)
	}
	if out, err := exec.Command("git", "-C", remote, "rev-parse", "--verify", "-q", "refs/heads/fix/login-fails-on-safari").CombinedOutput(); err != nil {
		t.Fatalf("the remote has no fix/login-fails-on-safari: %v\n%s", err, out)
	}
}

func pushGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=yardr", "GIT_AUTHOR_EMAIL=yardr@example.com",
		"GIT_COMMITTER_NAME=yardr", "GIT_COMMITTER_EMAIL=yardr@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
