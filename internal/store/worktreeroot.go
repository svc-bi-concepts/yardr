package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	bolt "go.etcd.io/bbolt"
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

// Settings returns every setting, the derived worktree_root_volume when
// recorded, and files_max: how many files one mail or note may carry.
// The latter two are metadata, not settings the operator changes.
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
	recorded := string(w.Bucket(bMeta).Get([]byte(WorktreeRootVolume)))
	if recorded != "" && !WorktreeMounted(recorded, recorded) {
		var previous string
		if _, err := get(w.Tx, bSettings, WorktreeRootSetting, &previous); err != nil {
			return err
		}
		same := filepath.Clean(root) == filepath.Clean(previous)
		if !same {
			oldPath, oldErr := filepath.EvalSymlinks(previous)
			newPath, newErr := filepath.EvalSymlinks(root)
			same = oldErr == nil && newErr == nil && oldPath == newPath
		}
		relative, err := filepath.Rel(recorded, root)
		beneath := err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
		if same || beneath {
			return nil
		}
	}
	volume, err := WorktreeVolume(root)
	if err != nil {
		return fmt.Errorf("worktree_root volume: %w", err)
	}
	return w.Bucket(bMeta).Put([]byte(WorktreeRootVolume), []byte(volume))
}
