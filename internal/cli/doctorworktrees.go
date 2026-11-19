package cli

import (
	"context"
	"fmt"
	"os"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
)

// worktreesState is where new bead worktrees are made, for doctor: the
// worktree_root setting when it names a directory, else the home's worktrees.
// A worktree that stands is not moved by the setting; only a new one is
// refused while the setting's directory is not there.
type worktreesState struct {
	Root  string `json:"root"`
	Set   bool   `json:"set"`
	There bool   `json:"there"`
}

// worktreesOf reads the setting and whether its directory is there. The
// home's worktrees are there when none is made yet, which is no fault: they
// are made on demand.
func worktreesOf(ctx context.Context, cfg *config.Config, st store.API) (*worktreesState, error) {
	all, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	if root := all[store.WorktreeRootSetting]; root != "" {
		fi, err := os.Stat(root)
		return &worktreesState{Root: root, Set: true, There: err == nil && fi.IsDir()}, nil
	}
	return &worktreesState{Root: cfg.Worktrees(), There: true}, nil
}

// line is doctor's line for the worktrees. A missing setting directory is a
// fault: every new bead worktree is refused until it is back, so the beads
// wait and nothing is made on the disk the setting was meant to leave.
func (w *worktreesState) line(yardr string) docLine {
	if !w.Set {
		return docLine{tagOK, "worktrees", fmt.Sprintf("new ones under %s (the home's; worktree_root unset)", w.Root)}
	}
	if !w.There {
		return docLine{tagFault, "worktrees", fmt.Sprintf("%s (worktree_root) is not there: a new bead worktree is refused until its volume is mounted. "+
			"To go back to the home's worktrees: %s settings set worktree_root \"\"", w.Root, yardr)}
	}
	return docLine{tagOK, "worktrees", fmt.Sprintf("new ones under %s (worktree_root), which is there", w.Root)}
}
