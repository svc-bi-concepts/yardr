package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

func TestRecoverDetachedWorktree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		moveRemote bool
		dirty      bool
		onBranch   bool
		wantErr    string
	}{
		{name: "origin head opens"},
		{name: "detached elsewhere fails", moveRemote: true, wantErr: "differs from origin/yard/r-1"},
		{name: "dirty worktree fails", dirty: true, wantErr: "worktree is dirty"},
		{name: "worktree on branch fails", onBranch: true, wantErr: "worktree is on a branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := seedRepo(t)
			origin := filepath.Join(t.TempDir(), "origin.git")
			gitT(t, repo, "init", "--bare", "-q", origin)
			gitT(t, repo, "remote", "add", "origin", origin)
			base := gitOutT(t, repo, "rev-parse", "HEAD")
			gitT(t, repo, "push", "-q", "origin", "HEAD:refs/heads/main", "HEAD:refs/heads/yard/r-1")
			if tc.moveRemote {
				gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "remote")
				gitT(t, repo, "push", "-q", "origin", "HEAD:refs/heads/yard/r-1")
			}
			gitT(t, repo, "fetch", "-q", "origin")
			// The checkout branch is stale while the registered worktree is
			// detached at the old origin head, as after a rewritten branch.
			gitT(t, repo, "branch", "yard/r-1", base)
			root := t.TempDir()
			wt := filepath.Join(root, "r", "r-1")
			gitT(t, repo, "worktree", "add", "-q", "--detach", wt, base)
			if tc.dirty {
				if err := os.WriteFile(filepath.Join(wt, "untracked"), []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.onBranch {
				gitT(t, wt, "checkout", "-q", "-b", "other")
			}
			log := filepath.Join(t.TempDir(), "calls")
			h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: pullHerdr(t, log), WorktreeRoot: root}
			depot := store.Depot{Name: "r", Path: repo, Base: "origin/main"}
			handle, err := h.Start(context.Background(), dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: "new"},
				Group: store.Group{Name: "builders"}, Depot: &depot, Session: store.Session{ID: "s-r-1"}})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("start = %v, want error containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			if handle["worktree"] != wt {
				t.Fatalf("opened worktree = %v, want %s", handle["worktree"], wt)
			}
			if got := gitOutT(t, wt, "symbolic-ref", "--short", "HEAD"); got != "yard/r-1" {
				t.Fatalf("worktree branch = %s, want yard/r-1", got)
			}
		})
	}
}
