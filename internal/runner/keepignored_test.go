package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
)

// keepRepo is a depot whose .gitignore holds private/ and node_modules/, with
// one worktree of bead b1 under root. The worktree holds ignored data, an
// ignored regenerable tree, an ignored credential and a tracked file.
func keepRepo(t *testing.T) (repo, wt, home string) {
	t.Helper()
	repo = seedRepo(t)
	gitT(t, repo, "config", "user.email", "t@example.test")
	gitT(t, repo, "config", "user.name", "t")
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("private/\nnode_modules/\n.env\n"), 0o600)
	gitT(t, repo, "add", ".gitignore")
	gitT(t, repo, "commit", "-q", "-m", "ignore")
	wt = filepath.Join(t.TempDir(), "depot", "b1")
	gitT(t, repo, "worktree", "add", "-q", "-b", "yard/b1", wt)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(wt, "private", "sub"), 0o700))
	must(os.WriteFile(filepath.Join(wt, "private", "x.csv"), []byte("a,b\n1,2\n"), 0o600))
	must(os.WriteFile(filepath.Join(wt, "private", "sub", "y.txt"), []byte("y"), 0o640))
	must(os.MkdirAll(filepath.Join(wt, "node_modules", "pkg"), 0o700))
	must(os.WriteFile(filepath.Join(wt, "node_modules", "pkg", "index.js"), []byte("x"), 0o600))
	must(os.WriteFile(filepath.Join(wt, ".env"), []byte("TOKEN=1"), 0o600))
	must(os.WriteFile(filepath.Join(wt, "tracked.txt"), []byte("t"), 0o600))
	gitT(t, wt, "add", "tracked.txt")
	gitT(t, wt, "commit", "-q", "-m", "tracked")
	return repo, wt, filepath.Join(t.TempDir(), "home")
}

func sha(t *testing.T, path string) [32]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

// Closing a bead moves its ignored data to the kept directory, byte for
// byte and with its mode, and removes the worktree; regenerable trees go
// with it, and a credential is left to go with the worktree.
func TestHerdrFinishKeepsIgnoredData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, wt, home := keepRepo(t)
	want := sha(t, filepath.Join(wt, "private", "x.csv"))
	h := &Herdr{YardHome: home}
	handle := map[string]any{"worktree": wt, "repo": repo, "depot": "depot", "bead": "b1"}
	if err := h.Finish(ctx, handle); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree not removed")
	}
	kept := filepath.Join(home, "kept", "depot", "b1")
	if got := sha(t, filepath.Join(kept, "private", "x.csv")); got != want {
		t.Fatal("kept file differs from the one in the worktree")
	}
	info, err := os.Stat(filepath.Join(kept, "private", "x.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("kept file mode = %v, want 0600", info.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(kept, "private", "sub", "y.txt")); err != nil {
		t.Fatalf("nested file not kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(kept, "node_modules")); !os.IsNotExist(err) {
		t.Fatal("a regenerable tree was copied")
	}
	if _, err := os.Stat(filepath.Join(kept, ".env")); !os.IsNotExist(err) {
		t.Fatal("a credential was kept")
	}
	gitT(t, repo, "rev-parse", "--verify", "yard/b1") // branch kept
}

// A name taken in the kept directory gets a numbered suffix: both files stay.
func TestKeepIgnoredNameCollision(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, wt, home := keepRepo(t)
	kept := filepath.Join(home, "kept", "depot", "b1")
	os.MkdirAll(filepath.Join(kept, "private"), 0o700)
	os.WriteFile(filepath.Join(kept, "private", "x.csv"), []byte("older"), 0o600)
	gitDir, err := gitOut(ctx, wt, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatal(err)
	}
	res, err := keepIgnored(ctx, gitDir, wt, kept)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || len(res.Left) != 1 {
		t.Fatalf("moved %d, left %v; want 2 moved and the credential left", res.Files, res.Left)
	}
	if got, _ := os.ReadFile(filepath.Join(kept, "private", "x.csv")); string(got) != "older" {
		t.Fatal("an existing kept file was overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(kept, "private", "x.csv.1")); string(got) != "a,b\n1,2\n" {
		t.Fatalf("the moved file is %q", got)
	}
}

// A move that cannot be made keeps the worktree and returns the error, so
// nothing is lost: the kept directory here is a file, so none can be made.
func TestHerdrFinishKeepsWorktreeOnFailedMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, wt, home := keepRepo(t)
	os.MkdirAll(filepath.Join(home, "kept"), 0o700)
	os.WriteFile(filepath.Join(home, "kept", "depot"), []byte("not a directory"), 0o600)
	h := &Herdr{YardHome: home}
	err := h.Finish(ctx, map[string]any{"worktree": wt, "repo": repo, "depot": "depot", "bead": "b1"})
	if err == nil || errors.Is(err, dispatch.ErrNothingLeft) {
		t.Fatalf("Finish = %v, want an error", err)
	}
	if strings.Contains(err.Error(), "x.csv") {
		t.Fatalf("the error names a file of private data: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "private", "x.csv")); err != nil {
		t.Fatal("private data was moved or deleted on a failed close")
	}
}

// A move-aside left by a cleanup that died keeps its ignored data before it
// is deleted.
func TestHerdrFinishKeepsLeftoverPrivateData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, wt, home := keepRepo(t)
	stale := wt + ".removing"
	if err := os.Rename(wt, stale); err != nil {
		t.Fatal(err)
	}
	h := &Herdr{YardHome: home}
	if err := h.Finish(ctx, map[string]any{"worktree": wt, "repo": repo, "depot": "depot", "bead": "b1"}); !errors.Is(err, dispatch.ErrNothingLeft) {
		t.Fatalf("Finish = %v, want ErrNothingLeft", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("leftover not removed")
	}
	if _, err := os.Stat(filepath.Join(home, "kept", "depot", "b1", "private", "sub", "y.txt")); err != nil {
		t.Fatalf("leftover's private data not kept: %v", err)
	}
}

// keptDir reads a worktree handle's depot and bead, and an older handle's
// from the worktree's path, which is <root>/<depot>/<bead>.
func TestKeptDirPerDepotAndBead(t *testing.T) {
	t.Parallel()
	h := &Herdr{YardHome: "/yard"}
	if got := h.keptDir(map[string]any{"depot": "d", "bead": "b"}, "/w/x/y"); got != filepath.Join("/yard", "kept", "d", "b") {
		t.Fatalf("keptDir = %q", got)
	}
	if got := h.keptDir(map[string]any{}, "/root/depot/b9"); got != filepath.Join("/yard", "kept", "depot", "b9") {
		t.Fatalf("keptDir from path = %q", got)
	}
	if got := (&Herdr{}).keptDir(map[string]any{"depot": "d", "bead": "b"}, "/w"); got != "" {
		t.Fatalf("keptDir without a yard home = %q", got)
	}
}

func TestIsCredentialAndNotKept(t *testing.T) {
	t.Parallel()
	for _, rel := range []string{".env", "private/.env.local", "a/b.key", "c.pem", "secret-id", "token"} {
		if !isCredential(rel) {
			t.Errorf("%s is not a credential", rel)
		}
	}
	for _, rel := range []string{"private/x.csv", "env.txt", "tokens.csv"} {
		if isCredential(rel) {
			t.Errorf("%s is a credential", rel)
		}
	}
}

// notKeptPath skips a regenerable tree only at the worktree root or beside a
// manifest, and never anything under a private directory. The web directory
// holds a manifest; scratch and app hold none.
func TestNotKeptPathRules(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		rel   string
		isDir bool
		skip  bool
	}{
		{"target", true, true},                    // at the root
		{"target/x.bin", false, true},             // a file in a root target
		{"web/node_modules", true, true},          // beside web/package.json
		{"web/target/a.o", false, true},           // a file in a manifest's target
		{"scratch/target", true, false},           // no manifest beside it: data
		{"scratch/target/data.csv", false, false}, // so is what is in it
		{"app/node_modules/x/y", true, false},     // no manifest beside app/
		{"private/finance/target", true, false},   // under private/: always kept
		{"a/private/node_modules", true, false},   // private anywhere on the path
		{"notes/target.txt", false, false},        // a file, not a tree named target
		{"private/node_modules_notes.txt", false, false},
	} {
		if got := notKeptPath(dir, c.rel, c.isDir); got != c.skip {
			t.Errorf("notKeptPath(%q, dir=%v) = %v, want %v", c.rel, c.isDir, got, c.skip)
		}
	}
}

// Ignored data kept on close: a target folder under private/ or in a folder
// with no manifest is data and is kept; a target at the root and a
// node_modules beside a manifest are regenerable and go with the worktree.
func TestKeepIgnoredTargetFoldersAreData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, wt, home := keepRepo(t)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(wt, ".gitignore"), []byte("private/\nnode_modules/\n.env\ntarget/\nscratch/\n"), 0o600))
	for rel, body := range map[string]string{
		"private/finance/target/budget.xlsx": "xlsx",
		"scratch/target/data.csv":            "a,b\n",
		"scratch/node_modules/x.js":          "x",
		"target/build.bin":                   "bin",
		"web/package.json":                   "{}",
		"web/node_modules/pkg/index.js":      "js",
	} {
		must(os.MkdirAll(filepath.Dir(filepath.Join(wt, rel)), 0o700))
		must(os.WriteFile(filepath.Join(wt, rel), []byte(body), 0o600))
	}
	gitDir, err := gitOut(ctx, wt, "rev-parse", "--absolute-git-dir")
	must(err)
	kept := filepath.Join(home, "kept", "depot", "b1")
	if _, err := keepIgnored(ctx, gitDir, wt, kept); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"private/finance/target/budget.xlsx",
		"scratch/target/data.csv",
		"scratch/node_modules/x.js",
	} {
		if _, err := os.Stat(filepath.Join(kept, rel)); err != nil {
			t.Errorf("data %s not kept: %v", rel, err)
		}
	}
	for _, rel := range []string{"target", "web/node_modules"} {
		if _, err := os.Stat(filepath.Join(kept, rel)); !os.IsNotExist(err) {
			t.Errorf("regenerable %s was copied", rel)
		}
	}
}
