package store

import (
	"fmt"
	"path/filepath"
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
	"the worktrees that stand keep their paths. A new worktree is refused while this directory is not there, so none is made on another volume: " +
	"mount the volume, or unset this setting to go back to the home's worktrees. Read when a worktree is made; unset: the home's worktrees",
	Check: func(v string) error {
		if !filepath.IsAbs(v) {
			return fmt.Errorf("%s %q: a directory is given as an absolute path", WorktreeRootSetting, v)
		}
		return nil
	}}
