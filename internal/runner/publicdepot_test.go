package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

// A script group in a public depot gets the bead's recorded branch as
// YARDR_BRANCH, the train's recorded branch as YARDR_TRAIN_BRANCH (the base
// a child starts from), and the depot's visibility; a private depot's bead
// with no record gets the yard's name and private. The landing scripts
// compare a wagon's base with YARDR_TRAIN_BRANCH, so a train whose name is
// not yard/<train> still matches.
func TestExecEnvPublicDepotNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		job  dispatch.Job
		want string
	}{
		{
			name: "a child of a train in a public depot",
			job: dispatch.Job{
				Bead:  store.Bead{ID: "yd-7", Stage: "approved", Type: "wagon", Branch: "fix/login"},
				Train: "yd-t1", Base: "feat/checkout-totals",
				Depot: &store.Depot{Name: "web", Base: "main", Visibility: store.DepotPublic},
			},
			want: "fix/login feat/checkout-totals public",
		},
		{
			name: "a bead with no record in a private depot",
			job: dispatch.Job{
				Bead:  store.Bead{ID: "yd-8", Stage: "approved"},
				Depot: &store.Depot{Name: "web", Base: "main", Visibility: store.DepotPrivate},
			},
			want: "yard/yd-8 private",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			out := filepath.Join(dir, "env")
			e := &Exec{Home: t.TempDir(), Bin: "/bin/yardr"}
			tc.job.Group = store.Group{Name: "assembly", Config: map[string]any{
				"cmd": "echo $YARDR_BRANCH $YARDR_TRAIN_BRANCH $YARDR_DEPOT_VISIBILITY > " + out, "log": filepath.Join(dir, "log")}}
			tc.job.Depot.Path = dir
			tc.job.Session = store.Session{ID: "s1"}
			if _, err := e.Start(context.Background(), tc.job); err != nil {
				t.Fatal(err)
			}
			var got string
			for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				b, _ := os.ReadFile(out)
				if got = strings.Join(strings.Fields(string(b)), " "); got == tc.want {
					return
				}
			}
			t.Fatalf("env = %q, want %q", got, tc.want)
		})
	}
}
