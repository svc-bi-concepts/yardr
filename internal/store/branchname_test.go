package store_test

import (
	"context"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// A bead made in a public depot records its branch: fix for a bug, feat for
// anything else, the title's slug after it, and no bead id anywhere in the
// name. A second bead with the same title and type gets -2. A bead in a
// private depot records nothing and keeps the yard's name for its branch.
func TestPublicDepotBeadRecordsItsBranch(t *testing.T) {
	t.Parallel()
	api, own := operatorYard(t)
	ctx := context.Background()
	repo := gitRepo(t, map[string]string{"README": "web\n"})
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: repo, Base: "main", Visibility: store.DepotPublic}, "jo"); err != nil {
		t.Fatal(err)
	}
	if err := api.PutDepot(ctx, store.Depot{Name: "api", Path: repo, Base: "main", Visibility: store.DepotPrivate}, "jo"); err != nil {
		t.Fatal(err)
	}
	create := func(depot, typ, title string) *store.Bead {
		t.Helper()
		b, err := api.CreateBead(ctx, store.NewBead{Title: title, Type: typ, Depot: depot}, "jo")
		if err != nil {
			t.Fatalf("create %q in %s: %v", title, depot, err)
		}
		return b
	}

	bug := create("web", "bug", "Login fails on Safari!")
	if want := "fix/login-fails-on-safari"; bug.Branch != want || store.BranchOfBead(*bug) != want {
		t.Fatalf("a bug's branch = %q (BranchOfBead %q), want %q", bug.Branch, store.BranchOfBead(*bug), want)
	}
	if strings.Contains(bug.Branch, bug.ID) {
		t.Fatalf("the branch %q names the bead %s", bug.Branch, bug.ID)
	}
	task := create("web", "task", "Login fails on Safari!")
	if want := "feat/login-fails-on-safari"; task.Branch != want {
		t.Fatalf("a task's branch = %q, want %q", task.Branch, want)
	}
	again := create("web", "bug", "Login fails on Safari!")
	if want := "fix/login-fails-on-safari-2"; again.Branch != want {
		t.Fatalf("a second bug with the same title = %q, want %q", again.Branch, want)
	}

	// Recorded once: a title edited afterwards leaves the branch as it is.
	if _, err := api.UpdateBead(ctx, bug.ID, store.BeadPatch{Title: strPtr("Something else")}, store.Expect{}, "jo"); err != nil {
		t.Fatal(err)
	}
	if got, err := api.GetBead(ctx, bug.ID); err != nil || got.Branch != "fix/login-fails-on-safari" {
		t.Fatalf("after a title edit the branch is %+v, %v; want it kept", got, err)
	}

	private := create("api", "bug", "Login fails on Safari!")
	if private.Branch != "" || store.BranchOfBead(*private) != "yard/"+private.ID {
		t.Fatalf("a private depot's bead records %q (branch %q), want none and yard/%s", private.Branch, store.BranchOfBead(*private), private.ID)
	}

	// The record is read back from the branch, which is how a webhook's
	// branch finds its bead; the yard's own names record nothing.
	if got, err := own.BeadOfBranch(ctx, "fix/login-fails-on-safari"); err != nil || got != bug.ID {
		t.Fatalf("BeadOfBranch(bug's branch) = %q, %v; want %s", got, err, bug.ID)
	}
	if got, err := own.BeadOfBranch(ctx, "fix/login-fails-on-safari-2"); err != nil || got != again.ID {
		t.Fatalf("BeadOfBranch(second bug's branch) = %q, %v; want %s", got, err, again.ID)
	}
	if got, err := own.BeadOfBranch(ctx, "yard/"+private.ID); err != nil || got != "" {
		t.Fatalf("BeadOfBranch(yard/<id>) = %q, %v; want nothing: that name records nothing", got, err)
	}
	if got, err := own.BeadOfBranch(ctx, "main"); err != nil || got != "" {
		t.Fatalf("BeadOfBranch(main) = %q, %v; want nothing", got, err)
	}
}

func strPtr(s string) *string { return &s }
