package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	bolt "go.etcd.io/bbolt"

	"yardr.dev/yardr/internal/config"
)

// WorktreeRootSetting is the directory new bead worktrees are made under
// (runner.Herdr). Unset, they go under the home's worktrees. A worktree that
// stands keeps its path: the root is read when a worktree is made, never to
// find one that was.
const WorktreeRootSetting = "worktree_root"

// worktreeRootSetting is the setting's entry, added to KnownSettings beside
// the others (settings.go). It is an absolute path only: a relative one would
// be read against whatever directory the daemon was started in.
var worktreeRootSetting = Setting{Key: WorktreeRootSetting, Help: "the directory new bead worktrees are made under, as an absolute path, such as /Volumes/Yard/worktrees/<yard>; " +
	"the worktrees that stand keep their paths. A new worktree is refused while this directory or its recorded mount is unavailable, so none is made on another volume: " +
	"mount the volume, or unset this setting to go back to the home's worktrees. Read when a worktree is made; unset: the home's worktrees",
	Check: func(v string) error {
		if !filepath.IsAbs(v) {
			return fmt.Errorf("%s %q: a directory is given as an absolute path", WorktreeRootSetting, v)
		}
		return nil
	}}

const WorktreeRootVolume = "worktree_root_volume"

// Settings returns every setting, and files_max beside them: how many
// files one mail or note may carry. That count is the yard's own, not a
// setting the operator changes, so a client needs no copy of the number.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	err := s.read(func(tx *bolt.Tx) error {
		if volume := tx.Bucket(bMeta).Get([]byte(WorktreeRootVolume)); len(volume) > 0 {
			out[WorktreeRootVolume] = string(volume)
		}
		return each(tx, bSettings, "", func(k []byte, v *string) error {
			out[string(k)] = *v
			return nil
		})
	})
	// The yard's own count wins over anything stored under its key.
	out[FilesMaxSetting] = strconv.Itoa(MaxMailFiles)
	return out, err
}

func (w *wtx) setWorktreeVolume(root string) error {
	if root == "" {
		return w.Bucket(bMeta).Delete([]byte(WorktreeRootVolume))
	}
	volume, err := config.WorktreeVolume(root)
	if err != nil {
		return fmt.Errorf("worktree_root volume: %w", err)
	}
	return w.Bucket(bMeta).Put([]byte(WorktreeRootVolume), []byte(volume))
}
