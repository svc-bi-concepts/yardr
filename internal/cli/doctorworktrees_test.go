package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// doctor says where new worktrees go: the home's while worktree_root is
// unset, the setting's directory when it is set, and a fault while that
// directory is not there.
func TestDoctorWorktreesLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := config.At(t.TempDir())
	st := storetest.New(t)

	w, err := worktreesOf(ctx, cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	if w.Set || w.Root != cfg.Worktrees() || !w.There {
		t.Fatalf("unset: %+v, want the home's worktrees", w)
	}
	if l := w.line("yardr"); l.tag != tagOK || !strings.Contains(l.text, cfg.Worktrees()) {
		t.Fatalf("unset line = %+v", l)
	}

	root := t.TempDir()
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, root, "me"); err != nil {
		t.Fatal(err)
	}
	if w, err = worktreesOf(ctx, cfg, st); err != nil {
		t.Fatal(err)
	}
	if !w.Set || w.Root != root || !w.There {
		t.Fatalf("set and there: %+v", w)
	}
	if l := w.line("yardr"); l.tag != tagOK {
		t.Fatalf("set and there line = %+v", l)
	}

	gone := filepath.Join(root, "not-mounted")
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, gone, "me"); err != nil {
		t.Fatal(err)
	}
	if w, err = worktreesOf(ctx, cfg, st); err != nil {
		t.Fatal(err)
	}
	if w.There {
		t.Fatalf("an absent root reads as there: %+v", w)
	}
	if l := w.line("yardr"); l.tag != tagFault || !strings.Contains(l.text, gone) {
		t.Fatalf("absent root line = %+v", l)
	}
}
