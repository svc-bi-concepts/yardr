package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	if p := h.recordedWorktree(ctx, job.Bead.ID); p != "" {
		return p, nil
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
			if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
				return "", dispatch.Blocked(fmt.Errorf("the worktree of %s is not made: %s (worktree_root) is not there, so its volume may not be mounted; "+
					"mount it, or unset the setting with yardr settings set %s \"\"", job.Bead.ID, root, store.WorktreeRootSetting))
			}
			return root, nil
		}
	}
	return h.WorktreeRoot, nil
}

// recordedWorktree is the path of the newest worktree a session of the bead
// recorded that still stands on disk, "" for none.
func (h *Herdr) recordedWorktree(ctx context.Context, bead string) string {
	if h.Store == nil {
		return ""
	}
	sessions, err := h.Store.ListSessions(ctx, store.SessionFilter{Bead: bead, All: true})
	if err != nil {
		return ""
	}
	best, at := "", time.Time{}
	for _, s := range sessions {
		p, _ := s.Handle["worktree"].(string)
		if p == "" || s.Cleaned || (best != "" && !s.StartedAt.After(at)) {
			continue
		}
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			continue
		}
		best, at = p, s.StartedAt
	}
	return best
}

// unmounted says a worktree at path that is missing lies under the
// worktree_root setting's directory, and that directory is not there: its
// volume is not mounted, so the worktree is not gone. A failed read of the
// setting is not taken for it, and the removal proceeds as before.
func (h *Herdr) unmounted(ctx context.Context, path string) bool {
	if h.Store == nil {
		return false
	}
	all, err := h.Store.Settings(ctx)
	if err != nil {
		return false
	}
	root := all[store.WorktreeRootSetting]
	if root == "" || !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return false
	}
	_, err = os.Stat(root)
	return os.IsNotExist(err)
}
