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
		name              string
		detachedElsewhere bool
		dirty             bool
		onBranch          bool
		rootName          string
		wantErr           string
	}{
		{name: "origin head opens"},
		{name: "non-ASCII path opens", rootName: "café"},
		{name: "detached elsewhere fails", detachedElsewhere: true, wantErr: "differs from origin/yard/r-1"},
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
			gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "published bead head")
			published := gitOutT(t, repo, "rev-parse", "HEAD")
			gitT(t, repo, "push", "-q", "origin", "HEAD:refs/heads/yard/r-1")
			gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "depot head")
			gitT(t, repo, "push", "-q", "origin", "HEAD:refs/heads/main")
			gitT(t, repo, "fetch", "-q", "origin")
			gitT(t, repo, "branch", "yard/r-1", base)
			root := filepath.Join(t.TempDir(), tc.rootName)
			wt := filepath.Join(root, "r", "r-1")
			detached := published
			if tc.detachedElsewhere {
				detached = base
			}
			gitT(t, repo, "worktree", "add", "-q", "--detach", wt, detached)
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
				if got := gitOutT(t, wt, "rev-parse", "HEAD"); got != detached {
					t.Fatalf("rejected worktree HEAD = %s, want %s", got, detached)
				}
				if got := gitOutT(t, repo, "rev-parse", "yard/r-1"); got != base {
					t.Fatalf("rejected branch = %s, want %s", got, base)
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
			if got := gitOutT(t, wt, "rev-parse", "HEAD"); got != published {
				t.Fatalf("recovered HEAD = %s, want published head %s", got, published)
			}
		})
	}
}
