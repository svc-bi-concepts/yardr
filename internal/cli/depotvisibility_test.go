package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// A depot is private unless the operator says it is public: depot add
// --visibility sets it, a re-add without the flag keeps what the depot has,
// depot visibility changes it alone, and show, list and --json all say it.
func TestDepotVisibilityCLI(t *testing.T) {
	t.Parallel()
	st := userYard(t)
	ctx := context.Background()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	gitIn(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "--allow-empty", "-m", "init")

	mustAs(t, st, "jo", "depot", "add", "web", repo, "--base", "main")
	if out := mustAs(t, st, "jo", "depot", "show", "web"); !strings.Contains(out, "\n  visibility: private\n") {
		t.Fatalf("depot show of a depot added with no flag = %q; want private", out)
	}
	if out := mustAs(t, st, "jo", "depot", "list"); !strings.Contains(out, "VISIBILITY") {
		t.Fatalf("depot list has no VISIBILITY column: %q", out)
	}

	if out := mustAs(t, st, "jo", "depot", "add", "web", repo, "--base", "main", "--visibility", "public"); !strings.Contains(out, "(git, base main)") {
		t.Fatalf("depot add --visibility public = %q", out)
	}
	if r, err := st.GetDepot(ctx, "web"); err != nil || r.Visibility != store.DepotPublic {
		t.Fatalf("web = %+v, %v; want public", r, err)
	}
	var shown store.Depot
	if out := mustAs(t, st, "jo", "depot", "show", "web", "--json"); json.Unmarshal([]byte(out), &shown) != nil || shown.Visibility != store.DepotPublic {
		t.Fatalf("depot show --json = %q; want visibility public", out)
	}

	// A re-add with no flag says nothing of visibility: the depot keeps it.
	mustAs(t, st, "jo", "depot", "add", "web", repo, "--base", "main")
	if r, err := st.GetDepot(ctx, "web"); err != nil || r.Visibility != store.DepotPublic {
		t.Fatalf("a re-add with no flag changed web to %+v, %v; want public kept", r, err)
	}

	if _, err := as(t, st, "jo", "depot", "add", "web", repo, "--base", "main", "--visibility", "secret"); err == nil {
		t.Fatal("depot add --visibility secret was accepted")
	}
	if _, err := as(t, st, "jo", "depot", "visibility", "web", "secret"); err == nil {
		t.Fatal("depot visibility web secret was accepted")
	}

	mustAs(t, st, "jo", "depot", "visibility", "web", "private")
	if r, err := st.GetDepot(ctx, "web"); err != nil || r.Visibility != store.DepotPrivate {
		t.Fatalf("after depot visibility web private: %+v, %v", r, err)
	}
	if out := mustAs(t, st, "jo", "depot", "show", "web"); !strings.Contains(out, "\n  visibility: private\n") {
		t.Fatalf("depot show after the change = %q", out)
	}

	// Only the operator changes it: a crew member's attempt is refused, and
	// the depot keeps what it was.
	if _, err := as(t, st, "yardmaster", "depot", "visibility", "web", "public"); err == nil {
		t.Fatal("depot visibility web public as the yardmaster was accepted")
	}
	if r, err := st.GetDepot(ctx, "web"); err != nil || r.Visibility != store.DepotPrivate {
		t.Fatalf("a refused visibility change changed web to %+v, %v", r, err)
	}
}
