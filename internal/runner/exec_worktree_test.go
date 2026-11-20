package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

func execEnvT(t *testing.T, e *Exec, job dispatch.Job, hookEnv []string) []string {
	t.Helper()
	env, err := e.env(context.Background(), job, hookEnv)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestExecWorktreeSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := storetest.New(t)
	depot := &store.Depot{Name: "r", Path: seedRepo(t), Base: "main"}
	mustT(t, s.PutDepot(ctx, *depot, "me"))
	_, err := s.CreateBead(ctx, store.NewBead{ID: "yd-7", Title: "one", Depot: "r"}, "me")
	mustT(t, err)
	home, root, own := t.TempDir(), t.TempDir(), t.TempDir()
	e := All(home, s, nil, nil)["exec"].(*Exec)
	for _, tc := range []struct {
		name, setting, groupRoot, want string
		blocked, dir                   bool
	}{
		{name: "unset", want: filepath.Join(home, "worktrees", "r", "yd-7")},
		{name: "set", setting: root, want: filepath.Join(root, "r", "yd-7")},
		{name: "group", setting: root, groupRoot: own, want: filepath.Join(own, "r", "yd-7")},
		{name: "unset again", want: filepath.Join(home, "worktrees", "r", "yd-7")},
		{name: "absent volume", setting: filepath.Join(root, "missing"), blocked: true},
		{name: "directory depot", setting: filepath.Join(root, "missing"), dir: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, tc.setting, "me"))
			d := *depot
			if tc.dir {
				d.Kind = store.DepotDir
			}
			job, out := execEnvJob(t, &d, "")
			if tc.groupRoot != "" {
				job.Group.Config["worktrees"] = tc.groupRoot
			}
			_, err := e.Start(ctx, job)
			if tc.blocked {
				if !dispatch.IsBlocked(err) {
					t.Fatalf("offline root: %v", err)
				}
				if _, err := os.Stat(out); !os.IsNotExist(err) {
					t.Fatalf("command ran with offline root: %v", err)
				}
				return
			}
			mustT(t, err)
			got := waitFile(t, out)
			if tc.dir {
				if strings.Contains(got, "YARDR_WORKTREE=") {
					t.Fatalf("directory depot received a worktree: %s", got)
				}
			} else if !strings.Contains(got, "YARDR_WORKTREE="+tc.want+"\n") {
				t.Fatalf("wrong worktree environment: %s", got)
			}
		})
	}
}

func TestExecUsesHerdrRecordedWorktree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := storetest.New(t)
	depot := &store.Depot{Name: "r", Path: seedRepo(t), Base: "main"}
	mustT(t, s.PutDepot(ctx, *depot, "me"))
	mustT(t, s.PutGroup(ctx, store.Group{Name: "g", Runner: "herdr", Limit: 9}, "me"))
	claimBead(t, s, "r-1")
	session, err := s.Claim(ctx, "r-1", "g", "new", "me")
	mustT(t, err)
	root := filepath.Join(t.TempDir(), "external worktrees")
	mustT(t, os.Mkdir(root, 0o755))
	mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, root, "me"))
	calls := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, calls), Bin: refusingWorktreeHerdr(t, calls), Store: &paneStore{API: s, t: t, calls: calls}}
	handle, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: "new"}, Group: store.Group{Name: "g"}, Depot: depot, Session: *session})
	mustT(t, err)
	mustT(t, s.SetHandle(ctx, session.ID, handle))
	e := All(t.TempDir(), s, nil, nil)["exec"].(*Exec)
	want := filepath.Join(root, "r", "r-1")
	for _, setting := range []string{root, t.TempDir(), "", filepath.Join(t.TempDir(), "offline")} {
		mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, setting, "me"))
		job, out := execEnvJob(t, depot, "")
		job.Bead.ID = "r-1"
		job.Group.Config["worktrees"] = t.TempDir()
		_, err := e.Start(ctx, job)
		mustT(t, err)
		got := waitFile(t, out)
		if !strings.Contains(got, "YARDR_WORKTREE="+want+"\n") {
			t.Fatalf("recorded path lost after setting %q: %s", setting, got)
		}
		if branch := gitOutT(t, want, "symbolic-ref", "--short", "HEAD"); branch != "yard/r-1" {
			t.Fatalf("assembly checkout branch: %s", branch)
		}
	}
}
