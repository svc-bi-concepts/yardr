package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
)

// Closing a bead names the directory its worktree's ignored data is kept in,
// one line, when the cleanup pass will keep some. The line names no file, and
// a bead whose worktree holds nothing to keep gets no line.
func TestBeadCloseNamesKeptDir(t *testing.T) {
	t.Parallel()
	st, ctx := reviewYard(t, "")
	home := t.TempDir()
	a := &app{st: st, actor: "me", cfg: &config.Config{Home: home}}

	repo := t.TempDir()
	gitIn := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.test",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitIn(repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("private/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(repo, "add", ".gitignore")
	gitIn(repo, "commit", "-q", "-m", "ignore")

	// closeWith makes a bead with one session whose worktree is wt, then
	// closes the bead, and returns the close's output.
	closeWith := func(t *testing.T, wt string) (*store.Bead, string) {
		t.Helper()
		b, err := st.CreateBead(ctx, store.NewBead{Title: "keep", Depot: "yard", Stage: "new"}, "me")
		if err != nil {
			t.Fatal(err)
		}
		sess, err := st.Claim(ctx, b.ID, "builders", "new", "yard")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetHandle(ctx, sess.ID, map[string]any{"worktree": wt, "repo": repo, "depot": "yard", "bead": b.ID}); err != nil {
			t.Fatal(err)
		}
		out, err := run(t, a.beadCmd(), "close", b.ID, "--reason", "merged")
		if err != nil {
			t.Fatalf("close %s: %v\n%s", b.ID, err, out)
		}
		return b, out
	}

	// A worktree with ignored private data: the close names its kept directory.
	wt := filepath.Join(t.TempDir(), "yard", "withdata")
	gitIn(repo, "worktree", "add", "-q", "--detach", wt, "HEAD")
	if err := os.MkdirAll(filepath.Join(wt, "private"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "private", "x.csv"), []byte("a,b\n1,2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, out := closeWith(t, wt)
	want := "kept: " + filepath.Join(home, "kept", "yard", b.ID) + "/\n"
	if !strings.Contains(out, want) {
		t.Fatalf("close output = %q, want the line %q", out, want)
	}
	if strings.Contains(out, "x.csv") {
		t.Fatalf("close output names a file of private data: %q", out)
	}
	if _, err := os.Stat(filepath.Join(wt, "private", "x.csv")); err != nil {
		t.Fatal("close moved private data: the cleanup pass, not close, keeps it")
	}

	// A worktree with nothing ignored to keep: no kept line.
	clean := filepath.Join(t.TempDir(), "yard", "clean")
	gitIn(repo, "worktree", "add", "-q", "--detach", clean, "HEAD")
	if _, out := closeWith(t, clean); strings.Contains(out, "kept:") {
		t.Fatalf("close of a clean worktree names a kept directory: %q", out)
	}
}
