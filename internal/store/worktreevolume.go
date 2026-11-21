package store

import (
	"os"
	"path/filepath"
)

func WorktreeVolume(path string) (string, error) {
	path = filepath.Clean(path)
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			return mountedVolume(resolved)
		}
		if !os.IsNotExist(err) || path == filepath.Dir(path) {
			return "", err
		}
		path = filepath.Dir(path)
	}
}

func WorktreeMounted(path, volume string) bool {
	fi, err := os.Stat(path)
	if err != nil || !fi.IsDir() {
		return false
	}
	if volume == "" {
		return false
	}
	actual, err := WorktreeVolume(path)
	return err == nil && actual == filepath.Clean(volume)
}

// worktreeRecordKept says a session's worktree record must outlive its
// cleanup, because the worktree is not known to be gone: it still stands on
// disk, or its volume is not mounted, so a removal did not happen (or could
// not). Then the record is the only way a later stage finds the worktree,
// even under a worktree_root that has since moved. Once the worktree is gone
// from a mounted volume, nothing needs the record.
func worktreeRecordKept(handle map[string]any) bool {
	path, _ := handle["worktree"].(string)
	if path == "" {
		return false
	}
	// Only a path that is certainly absent counts as gone: any other error
	// from the stat (permissions, say) keeps the record.
	if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
		return true
	}
	volume, _ := handle["worktree_volume"].(string)
	return volume != "" && !WorktreeMounted(volume, volume)
}
