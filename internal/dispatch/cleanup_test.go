package dispatch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

type cleanupResultRunner struct {
	*fakeRunner
	result error
}

func (r *cleanupResultRunner) Finish(context.Context, map[string]any) error { return r.result }

func TestCleanupRetiresOnlyRemovedWorktreeLocations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		result           error
		disabled, retire bool
	}{
		{name: "removed", retire: true},
		{name: "already absent", result: dispatch.ErrNothingLeft, retire: true},
		{name: "dirty", result: errors.New("uncommitted changes")},
		{name: "offline", result: dispatch.ErrWorktreeOffline},
		{name: "disabled", disabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := storetest.New(t)
			config := map[string]any{}
			if tc.disabled {
				config["cleanup"] = false
			}
			must(t, s.PutGroup(ctx, store.Group{Name: "g", Runner: "fake", Limit: 9, Config: config}, "me"))
			b, err := s.CreateBead(ctx, store.NewBead{Title: "one"}, "me")
			must(t, err)
			root := t.TempDir()
			alias := filepath.Join(t.TempDir(), "alias")
			must(t, os.Symlink(root, alias))
			path := filepath.Join(root, "worktree")
			var matching []string
			add := func(path string, cleaned bool) string {
				t.Helper()
				session, err := s.Claim(ctx, b.ID, "g", b.Stage, "me")
				must(t, err)
				must(t, s.SetHandle(ctx, session.ID, map[string]any{"worktree": path, "worktree_volume": root, "branch": "keep"}))
				must(t, s.EndSession(ctx, session.ID, "aborted", "test"))
				must(t, s.MarkStopped(ctx, session.ID))
				if cleaned {
					must(t, s.MarkCleaned(ctx, session.ID))
				}
				return session.ID
			}
			matching = append(matching, add(path, true), add(path+string(filepath.Separator), true), add(filepath.Join(alias, "worktree"), false))
			other := add(filepath.Join(root, "other"), true)
			_, err = s.CloseBead(ctx, b.ID, "done", store.Expect{}, "me")
			must(t, err)
			r := &cleanupResultRunner{fakeRunner: &fakeRunner{dead: map[string]bool{}}, result: tc.result}
			d := &dispatch.Dispatcher{Store: s, Runners: map[string]dispatch.Runner{"fake": r}}
			must(t, d.Reconcile(ctx))
			for _, id := range matching {
				session, err := s.GetSession(ctx, id)
				must(t, err)
				_, pathKept := session.Handle["worktree"]
				_, volumeKept := session.Handle["worktree_volume"]
				if pathKept == tc.retire || volumeKept == tc.retire {
					t.Fatalf("%s: path retained=%t, volume retained=%t, removal=%t", id, pathKept, volumeKept, tc.retire)
				}
				if session.Handle["branch"] != "keep" {
					t.Fatal("retirement changed unrelated handle data")
				}
			}
			sibling, err := s.GetSession(ctx, other)
			must(t, err)
			if sibling.Handle["worktree"] != filepath.Join(root, "other") || sibling.Handle["worktree_volume"] != root {
				t.Fatal("retirement changed another worktree")
			}
		})
	}
}
