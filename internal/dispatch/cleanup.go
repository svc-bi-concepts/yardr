package dispatch

import (
	"context"
	"errors"
	"maps"
	"path/filepath"

	"yardr.dev/yardr/internal/store"
)

var ErrWorktreeOffline = errors.New("worktree volume is offline")

// cleanup removes what runners left behind for beads that are now closed,
// unless the group sets cleanup=false.
func (d *Dispatcher) cleanup(ctx context.Context, groups map[string]store.Group) error {
	done, err := d.Store.ClosedWithWorkspace(ctx)
	if err != nil {
		return err
	}
	for _, s := range done {
		g := groups[s.Group]
		if f, ok := d.runnerFor(s, groups).(Finisher); ok && g.Config["cleanup"] != false {
			err := f.Finish(ctx, s.Handle)
			if err == nil || errors.Is(err, ErrNothingLeft) {
				if err := d.retireWorktree(ctx, s); err != nil {
					return err
				}
			}
			if errors.Is(err, ErrNothingLeft) {
				// another stage's session already handled it
			} else if errors.Is(err, ErrWorktreeOffline) {
				continue
			} else if err != nil {
				d.Log.Warn("workspace kept", "bead", s.Bead, "session", s.ID, "err", err)
				_ = d.Store.LogDispatch(ctx, s.Bead, "workspace_kept", actor, map[string]any{
					"session": s.ID, "worktree": s.Handle["worktree"], "reason": err.Error()})
			} else {
				_ = d.Store.LogDispatch(ctx, s.Bead, "workspace_removed", actor, map[string]any{
					"session": s.ID, "worktree": s.Handle["worktree"]})
			}
		}
		if err := d.Store.MarkCleaned(ctx, s.ID); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dispatcher) retireWorktree(ctx context.Context, removed store.Session) error {
	path, _ := removed.Handle["worktree"].(string)
	if path == "" {
		return nil
	}
	sessions, err := d.Store.ListSessions(ctx, store.SessionFilter{Bead: removed.Bead, All: true})
	if err != nil {
		return err
	}
	path = cleanupPath(path)
	for _, s := range sessions {
		recorded, _ := s.Handle["worktree"].(string)
		if recorded == "" || cleanupPath(recorded) != path {
			continue
		}
		handle := maps.Clone(s.Handle)
		delete(handle, "worktree")
		delete(handle, "worktree_volume")
		if err := d.Store.SetHandle(ctx, s.ID, handle); err != nil {
			return err
		}
	}
	return nil
}

func cleanupPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(parent, filepath.Base(path))
	}
	return path
}
