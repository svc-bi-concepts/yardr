package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestWorktreeRootPreservesOfflineMount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(ctx) })
	mount := filepath.Join(t.TempDir(), "ssd")
	root := filepath.Join(mount, "worktrees")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, WorktreeRootSetting, root, "me"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bMeta).Put([]byte(WorktreeRootVolume), []byte(mount))
	}); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, root + string(filepath.Separator), filepath.Join(root, "..", "worktrees"), alias, filepath.Join(root, "new")} {
		if err := s.SetSetting(ctx, WorktreeRootSetting, path, "me"); err != nil {
			t.Fatal(err)
		}
		all, err := s.Settings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if all[WorktreeRootVolume] != mount {
			t.Fatalf("reapplying %s replaced offline mount %s with %s", path, mount, all[WorktreeRootVolume])
		}
		if WorktreeMounted(root, all[WorktreeRootVolume]) {
			t.Fatal("unmounted directory accepted as the recorded mount")
		}
	}
	replacement := t.TempDir()
	if err := s.SetSetting(ctx, WorktreeRootSetting, replacement, "me"); err != nil {
		t.Fatal(err)
	}
	all, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all[WorktreeRootVolume] == mount || !WorktreeMounted(replacement, all[WorktreeRootVolume]) {
		t.Fatalf("new root's mounted volume was not recorded: %v", all)
	}
	if err := s.SetSetting(ctx, WorktreeRootSetting, "", "me"); err != nil {
		t.Fatal(err)
	}
	if all, err := s.Settings(ctx); err != nil || all[WorktreeRootVolume] != "" {
		t.Fatalf("unset left volume metadata: %v, %v", all, err)
	}
}
