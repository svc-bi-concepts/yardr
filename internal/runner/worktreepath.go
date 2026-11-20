package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

// worktreePath is where a herdr session's bead worktree is in a git depot:
// the path a live worktree of the bead was recorded at, else a new one under
// the root (worktreeRoot). "" when there is no root to make one under, which
// is then left to herdr.
//
// A recorded path wins over the root, so a bead's worktree stays where it is
// when the root moves: a later stage reopens it there, and a second worktree
// is never made for the bead's branch beside it. A recorded path that is no
// longer there is not reused, as before: the bead is set up again under the
// root.
func (h *Herdr) worktreePath(ctx context.Context, job dispatch.Job, depot string) (string, error) {
	if p, err := h.recordedWorktree(ctx, job.Bead.ID); p != "" || err != nil {
		return p, err
	}
	root, err := h.worktreeRoot(ctx, job)
	if err != nil || root == "" {
		return "", err
	}
	return filepath.Join(root, depot, job.Bead.ID), nil
}

// worktreeRoot is the directory a new bead worktree is made under: the
// group's worktrees, else the yard's worktree_root setting, else the root the
// runner was given. Only the setting is checked to be there. A root the
// operator set on purpose may sit on a volume that is not mounted, and the
// worktree must not then be made in an empty directory on the disk the
// setting was meant to leave: the session is refused instead, and it starts
// once the volume is back.
func (h *Herdr) worktreeRoot(ctx context.Context, job dispatch.Job) (string, error) {
	if root := str(job.Group.Config, "worktrees", ""); root != "" {
		return root, nil
	}
	if h.Store != nil {
		all, err := h.Store.Settings(ctx)
		if err != nil {
			return "", fmt.Errorf("worktree_root: %w", err)
		}
		if root := all[store.WorktreeRootSetting]; root != "" {
			if !config.WorktreeMounted(root, all[store.WorktreeRootVolume]) {
				return "", dispatch.Blocked(fmt.Errorf("the worktree of %s is not made: %s (worktree_root) is unavailable or its recorded volume is not mounted; "+
					"mount it, or unset the setting with yardr settings set %s \"\"", job.Bead.ID, root, store.WorktreeRootSetting))
			}
			return root, nil
		}
	}
	return h.WorktreeRoot, nil
}

// recordedWorktree is the path of the newest worktree a session of the bead
// recorded that still stands on disk, "" for none.
func (h *Herdr) recordedWorktree(ctx context.Context, bead string) (string, error) {
	if h.Store == nil {
		return "", nil
	}
	sessions, err := h.Store.ListSessions(ctx, store.SessionFilter{Bead: bead, All: true})
	if err != nil {
		return "", err
	}
	best, at := store.Session{}, time.Time{}
	for _, s := range sessions {
		p, _ := s.Handle["worktree"].(string)
		if p != "" && (best.Handle == nil || s.StartedAt.After(at)) {
			best, at = s, s.StartedAt
		}
	}
	if err := worktreeOffline(best.Handle); err != nil {
		return "", dispatch.Blocked(err)
	}
	path, _ := best.Handle["worktree"].(string)
	if path == "" {
		return "", nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	return path, nil
}

func worktreeOffline(handle map[string]any) error {
	path, _ := handle["worktree"].(string)
	volume, _ := handle["worktree_volume"].(string)
	if volume == "" {
		return nil
	}
	actual, err := config.WorktreeVolume(path)
	if !config.WorktreeMounted(volume, volume) || err != nil || actual != volume {
		return fmt.Errorf("worktree %s: volume %s is not mounted: %w", path, volume, dispatch.ErrWorktreeOffline)
	}
	return nil
}

func worktreeVolume(handle map[string]any) {
	path, _ := handle["worktree"].(string)
	if volume, err := config.WorktreeVolume(path); err == nil {
		handle["worktree_volume"] = volume
	}
}
