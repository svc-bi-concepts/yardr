package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// worktree_root is set to an absolute directory and unset by an empty value;
// a relative one is refused, since the daemon would read it against its own
// working directory.
func TestWorktreeRootSetting(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "worktrees")
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, root, "me"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Settings(ctx); got[store.WorktreeRootSetting] != root {
		t.Fatalf("worktree_root = %q, want %q", got[store.WorktreeRootSetting], root)
	}
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, "worktrees", "me"); err == nil {
		t.Fatal("a relative worktree_root was accepted")
	}
	if got, _ := st.Settings(ctx); got[store.WorktreeRootSetting] != root {
		t.Fatalf("a refused value changed the setting to %q", got[store.WorktreeRootSetting])
	}
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, "", "me"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Settings(ctx); got[store.WorktreeRootSetting] != "" {
		t.Fatalf("after unset: worktree_root = %q", got[store.WorktreeRootSetting])
	}
	if _, ok := store.SettingOf(store.WorktreeRootSetting); !ok {
		t.Fatal("worktree_root is not a known setting")
	}
}
