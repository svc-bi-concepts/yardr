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

	settings, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings[store.WorktreeRootVolume] = root
	offline, err := worktreesOf(ctx, cfg, &doctorWorktreeSettings{API: st, settings: settings})
	if err != nil {
		t.Fatal(err)
	}
	if offline.There || offline.line("yardr").tag != tagFault {
		t.Fatalf("unmounted directory reported healthy: %+v", offline)
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

type doctorWorktreeSettings struct {
	store.API
	settings map[string]string
}

func (s *doctorWorktreeSettings) Settings(context.Context) (map[string]string, error) {
	return s.settings, nil
}

func TestWorktreeMetadataExcludedFromSettings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st := storetest.New(t)
	if err := st.SetSetting(ctx, store.WorktreeRootSetting, t.TempDir(), "me"); err != nil {
		t.Fatal(err)
	}
	settings, err := st.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if settings[store.WorktreeRootVolume] == "" {
		t.Fatal("missing mount metadata")
	}
	settings["retired_setting"] = "old"
	notes, err := configNotes(ctx, &doctorWorktreeSettings{API: st, settings: settings}, nil, nil, "yardr")
	if err != nil {
		t.Fatal(err)
	}
	retired := false
	for _, note := range notes {
		if strings.Contains(note, store.WorktreeRootVolume) {
			t.Fatalf("mount metadata reported as obsolete: %s", note)
		}
		retired = retired || strings.Contains(note, "retired_setting")
	}
	if !retired {
		t.Fatal("obsolete stored setting no longer reported")
	}
	root, obsolete := false, false
	for _, row := range settingRows(t.TempDir(), settings) {
		if row.Key == store.WorktreeRootVolume {
			t.Fatalf("metadata exposed as a setting row: %+v", row)
		}
		root = root || row.Key == store.WorktreeRootSetting
		obsolete = obsolete || row.Key == "retired_setting"
	}
	if !root || !obsolete {
		t.Fatal("settings rows lost the root or obsolete stored setting")
	}
}
