package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// A new bead worktree is made under the yard's worktree_root setting when it
// is set, and under the runner's own root when it is not. A group's own
// worktrees setting still wins over both.
func TestWorktreeRootSetting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	depot := store.Depot{Name: "r", Path: repo, Base: "main"}
	s := storetest.New(t)
	if err := s.PutDepot(ctx, depot, "me"); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	home := t.TempDir()
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: worktreeHerdr(t, log), WorktreeRoot: home, Store: &paneStore{API: s, t: t, calls: log}}
	own := t.TempDir()
	mustT(t, s.PutGroup(ctx, store.Group{Name: "g", Runner: "herdr", Limit: 9}, "me"))
	mustT(t, s.PutGroup(ctx, store.Group{Name: "own", Runner: "herdr", Limit: 9, Config: map[string]any{"worktrees": own}}, "me"))
	start := func(bead string, group store.Group) string {
		t.Helper()
		claimBead(t, s, bead)
		sess, err := s.Claim(ctx, bead, group.Name, "new", "me")
		if err != nil {
			t.Fatal(err)
		}
		handle, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: bead, Stage: "new"}, Group: group,
			Depot: &depot, Session: store.Session{ID: sess.ID}})
		if err != nil {
			t.Fatal(err)
		}
		return handle["worktree"].(string)
	}

	// Unset: the runner's root.
	if got, want := start("r-1", store.Group{Name: "g"}), filepath.Join(home, "r", "r-1"); got != want {
		t.Fatalf("unset: worktree at %s, want %s", got, want)
	}

	root := t.TempDir()
	if err := s.SetSetting(ctx, store.WorktreeRootSetting, root+string(filepath.Separator), "me"); err != nil {
		t.Fatal(err)
	}
	if got, want := start("r-2", store.Group{Name: "g"}), filepath.Join(root, "r", "r-2"); got != want {
		t.Fatalf("set: worktree at %s, want %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "r", "r-2", ".git")); err != nil {
		t.Fatalf("no worktree on the root's volume: %v", err)
	}

	// The group's own worktrees setting is the one that is read before it.
	if got, want := start("r-3", store.Group{Name: "own", Config: map[string]any{"worktrees": own}}), filepath.Join(own, "r", "r-3"); got != want {
		t.Fatalf("group worktrees: worktree at %s, want %s", got, want)
	}

	// Unset again: the next new worktree goes back to the runner's root.
	if err := s.SetSetting(ctx, store.WorktreeRootSetting, "", "me"); err != nil {
		t.Fatal(err)
	}
	if got, want := start("r-4", store.Group{Name: "g"}), filepath.Join(home, "r", "r-4"); got != want {
		t.Fatalf("unset again: worktree at %s, want %s", got, want)
	}
}

// While the root's directory is not there (its volume is not mounted), a
// new worktree is refused: nothing is made, and herdr is not asked for one,
// so the home's disk is not written in its place.
func TestWorktreeRootAbsentRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	depot := store.Depot{Name: "r", Path: repo, Base: "main"}
	s := storetest.New(t)
	if err := s.PutDepot(ctx, depot, "me"); err != nil {
		t.Fatal(err)
	}
	claimBead(t, s, "r-1")
	gone := filepath.Join(t.TempDir(), "Volumes", "Yard", "worktrees")
	if err := s.SetSetting(ctx, store.WorktreeRootSetting, gone, "me"); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: worktreeHerdr(t, log), WorktreeRoot: t.TempDir(), Store: s}
	_, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: "new"}, Group: store.Group{Name: "g"},
		Depot: &depot, Session: store.Session{ID: "s"}})
	if !dispatch.IsBlocked(err) {
		t.Fatalf("Start with an absent root: err = %v, want a blocked session", err)
	}
	if !strings.Contains(err.Error(), gone) || !strings.Contains(err.Error(), "worktree_root") {
		t.Errorf("refusal does not name the root: %v", err)
	}
	if _, serr := os.Stat(gone); serr == nil {
		t.Errorf("the absent root was created")
	}
	if raw, _ := os.ReadFile(log); strings.Contains(string(raw), "worktree create") {
		t.Errorf("herdr was asked for a worktree:\n%s", raw)
	}
}

// refusingWorktreeHerdr is worktreeHerdr, except that herdr answers a create
// it cannot do (the branch or the path is taken) with its own error, which
// the runner then opens the worktree in place of.
func refusingWorktreeHerdr(t *testing.T, log string) string {
	return fakeHerdrBin(t, `printf '%s\n' "$*" >> `+log+`
case "$1 $2" in
  "worktree create")
    shift 2
    while [ $# -gt 1 ]; do
      case "$1" in --cwd) cwd=$2 ;; --branch) br=$2 ;; --base) base=$2 ;; --path) p=$2 ;; esac
      shift 2
    done
    if git -C "$cwd" worktree add -q -b "$br" "$p" "$base" >&2; then
      echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"'"$p"'"}}}'
    else
      echo '{"error":{"code":"worktree_exists","message":"the branch or the path is taken"}}'
    fi ;;
  "worktree open")
    shift 2
    while [ $# -gt 1 ]; do
      case "$1" in --cwd) cwd=$2 ;; --path) p=$2 ;; --branch) br=$2 ;; esac
      shift 2
    done
    if [ -z "$p" ]; then
      p=$(git -C "$cwd" worktree list --porcelain | awk -v b="refs/heads/$br" '/^worktree /{w=substr($0,10)} $0=="branch "b{print w}')
    fi
    if [ -n "$p" ]; then
      echo '{"result":{"workspace":{"workspace_id":"w1"},"tab":{"tab_id":"t1"},"root_pane":{"pane_id":"p1"},"worktree":{"path":"'"$p"'"}}}'
    else
      echo '{"error":{"code":"not_found","message":"no worktree of that branch"}}'
    fi ;;
  "pane process-info") echo '{"result":{"process_info":{"pane_id":"p1","shell_pid":`+fmt.Sprint(os.Getpid())+`}}}' ;;
  *) echo '{"result":{"type":"ok"}}' ;;
esac`)
}

// claimBead puts a bead of depot r on record, for a session to start on.
func claimBead(t *testing.T, s store.API, id string) {
	t.Helper()
	if _, err := s.CreateBead(context.Background(), store.NewBead{ID: id, Title: id, Depot: "r"}, "me"); err != nil {
		t.Fatal(err)
	}
}

// A bead's worktree keeps the path its session recorded when the root moves:
// a later stage reopens it there and makes no second one under the new root,
// and removing it at that path works as before.
func TestWorktreeRecordedPathKept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	depot := store.Depot{Name: "r", Path: repo, Base: "main"}
	s := storetest.New(t)
	if err := s.PutDepot(ctx, depot, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateBead(ctx, store.NewBead{ID: "r-1", Title: "one", Depot: "r"}, "me"); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "calls")
	old, root := t.TempDir(), t.TempDir()
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: refusingWorktreeHerdr(t, log), WorktreeRoot: old, Store: &paneStore{API: s, t: t, calls: log}}
	mustT(t, s.PutGroup(ctx, store.Group{Name: "g", Runner: "herdr", Limit: 9}, "me"))
	// start runs the bead's session sess, which the store holds with the
	// handle of its last start. A claim takes an unowned bead only, so a
	// later stage reuses the session the first one claimed.
	start := func(sess, stageName string) map[string]any {
		t.Helper()
		handle, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: stageName}, Group: store.Group{Name: "g"},
			Depot: &depot, Session: store.Session{ID: sess}})
		if err != nil {
			t.Fatal(err)
		}
		return handle
	}
	claimed, err := s.Claim(ctx, "r-1", "g", "new", "me")
	if err != nil {
		t.Fatal(err)
	}
	first := start(claimed.ID, "new")
	mustT(t, s.SetHandle(ctx, claimed.ID, first))
	wt := first["worktree"].(string)
	if want := filepath.Join(old, "r", "r-1"); wt != want {
		t.Fatalf("first stage at %s, want %s", wt, want)
	}
	if err := s.SetSetting(ctx, store.WorktreeRootSetting, root+string(filepath.Separator), "me"); err != nil {
		t.Fatal(err)
	}

	mustT(t, s.MarkCleaned(ctx, claimed.ID))

	// The root moved: the next stage reopens the worktree where it stands.
	next := start(claimed.ID, "review")
	// git reports the worktree by its resolved path (/private/tmp on macOS).
	if got, want := mustEval(t, next["worktree"].(string)), mustEval(t, wt); got != want {
		t.Fatalf("later stage at %s, want the recorded %s", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "r", "r-1")); err == nil {
		t.Errorf("a second worktree of the bead was made under the new root")
	}

	// Removal goes to the recorded path.
	if err := h.Finish(ctx, first); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Errorf("the worktree at its recorded path was not removed")
	}
}

// A worktree on a volume that is not mounted is not removed: its git record
// stays, so nothing is pruned, and it is removed at its path once the volume
// is back.
func TestWorktreeRemovalWaitsForVolume(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo := seedRepo(t)
	depot := store.Depot{Name: "r", Path: repo, Base: "main"}
	s := storetest.New(t)
	mustT(t, s.PutDepot(ctx, depot, "me"))
	mustT(t, s.PutGroup(ctx, store.Group{Name: "g", Runner: "herdr", Limit: 2}, "me"))
	claimBead(t, s, "r-1")
	log := filepath.Join(t.TempDir(), "calls")
	h := &Herdr{Socket: fakeHerdrAPI(t, log), Bin: refusingWorktreeHerdr(t, log), WorktreeRoot: t.TempDir(), Store: &paneStore{API: s, t: t, calls: log}}
	sess, err := s.Claim(ctx, "r-1", "g", "new", "me")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "volume")
	mustT(t, os.Mkdir(root, 0o755))
	mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, root+string(filepath.Separator), "me"))
	handle, err := h.Start(ctx, dispatch.Job{Bead: store.Bead{ID: "r-1", Stage: "new"}, Group: store.Group{Name: "g"},
		Depot: &depot, Session: store.Session{ID: sess.ID}})
	if err != nil {
		t.Fatal(err)
	}
	wt := handle["worktree"].(string)

	volume := handle["worktree_volume"]
	away := root + ".away"
	mustT(t, os.Rename(root, away))
	mustT(t, os.Mkdir(root, 0o755))
	gitT(t, repo, "config", "gc.worktreePruneExpire", "now")
	handle["worktree_volume"] = root
	mustT(t, s.SetHandle(ctx, sess.ID, handle))
	mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, t.TempDir(), "me"))
	_, err = s.CloseBead(ctx, "r-1", "done", store.Expect{}, "me")
	mustT(t, err)
	d := &dispatch.Dispatcher{Store: s, Runners: map[string]dispatch.Runner{"herdr": h}}
	mustT(t, d.Reconcile(ctx))
	kept, err := s.GetSession(ctx, sess.ID)
	mustT(t, err)
	if kept.Cleaned {
		t.Fatal("offline worktree marked cleaned")
	}
	mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, "", "me"))
	mustT(t, d.Reconcile(ctx))
	if !strings.Contains(gitOutT(t, repo, "worktree", "list", "--porcelain"), wt) {
		t.Fatalf("git's record of %s was pruned while its volume was away", wt)
	}
	if _, err := worktreePath(ctx, h.Store, h.WorktreeRoot, dispatch.Job{Bead: store.Bead{ID: "r-1"}}, "r"); !dispatch.IsBlocked(err) {
		t.Fatalf("offline recorded path: %v", err)
	}
	assembly := All(t.TempDir(), s, nil, nil)["exec"].(*Exec)
	assemblyJob, output := execEnvJob(t, &depot, "")
	assemblyJob.Bead.ID = "r-1"
	if _, err := assembly.Start(ctx, assemblyJob); !dispatch.IsBlocked(err) {
		t.Fatalf("assembly accepted offline recorded worktree: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("assembly ran while recorded volume was offline: %v", err)
	}
	for _, name := range []string{"present", "missing"} {
		sibling := filepath.Join(t.TempDir(), name)
		gitT(t, repo, "worktree", "add", "-b", name, sibling, "main")
		if name == "missing" {
			mustT(t, os.RemoveAll(sibling))
		}
		err := h.Finish(ctx, map[string]any{"worktree": sibling, "repo": repo})
		if err != nil && !errors.Is(err, dispatch.ErrNothingLeft) {
			t.Fatal(err)
		}
		if !strings.Contains(gitOutT(t, repo, "worktree", "list", "--porcelain"), wt) {
			t.Fatal("sibling cleanup pruned offline worktree")
		}
	}
	mustT(t, os.Remove(root))
	mustT(t, os.Rename(away, root))
	handle["worktree_volume"] = volume
	mustT(t, s.SetHandle(ctx, sess.ID, handle))
	mustT(t, d.Reconcile(ctx))
	cleaned, err := s.GetSession(ctx, sess.ID)
	mustT(t, err)
	if !cleaned.Cleaned {
		t.Fatal("remounted worktree was not cleaned")
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree remains: %v", err)
	}
}

func TestWorktreeRootUnmountedDirectoryRefuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := storetest.New(t)
	_, err := s.CreateBead(ctx, store.NewBead{ID: "r-1", Title: "one"}, "me")
	mustT(t, err)
	root := t.TempDir()
	mustT(t, s.SetSetting(ctx, store.WorktreeRootSetting, root+string(filepath.Separator), "me"))
	settings, err := s.Settings(ctx)
	mustT(t, err)
	settings[store.WorktreeRootVolume] = root
	h := &Herdr{Store: &worktreeSettings{API: s, settings: settings}}
	_, err = worktreePath(ctx, h.Store, h.WorktreeRoot, dispatch.Job{Bead: store.Bead{ID: "r-1"}}, "r")
	if !dispatch.IsBlocked(err) {
		t.Fatalf("unmounted directory accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "r")); !os.IsNotExist(err) {
		t.Fatalf("wrote under unmounted root: %v", err)
	}
	volume, err := config.WorktreeVolume(root)
	mustT(t, err)
	if !config.WorktreeMounted(root, volume) {
		t.Fatal("real mounted volume refused")
	}
	if config.WorktreeMounted(root, root) {
		t.Fatal("ordinary directory accepted as a mount")
	}
}

type worktreeSettings struct {
	store.API
	settings map[string]string
}

func (s *worktreeSettings) Settings(context.Context) (map[string]string, error) {
	return s.settings, nil
}
