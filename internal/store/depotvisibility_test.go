package store_test

import (
	"context"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// A depot is private unless the operator makes it public: a depot put with
// no visibility reads as private, and a re-put that leaves it as it is stays
// anyone's, as a re-put with the kind unchanged does.
func TestDepotVisibilityDefaultsPrivate(t *testing.T) {
	t.Parallel()
	api, _ := operatorYard(t)
	ctx := context.Background()
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: "/srv/web"}, "jo"); err != nil {
		t.Fatal(err)
	}
	if d, err := api.GetDepot(ctx, "web"); err != nil || d.Visibility != store.DepotPrivate {
		t.Fatalf("a depot with no visibility = %+v, %v; want private", d, err)
	}
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: "/srv/web", Visibility: store.DepotPrivate}, "yardmaster"); err != nil {
		t.Fatalf("re-putting a private depot as yardmaster: %v", err)
	}
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: "/srv/web", Visibility: "secret"}, "jo"); err == nil {
		t.Fatal("a visibility that is neither public nor private was stored")
	}
	ds, err := api.Depots(ctx)
	if err != nil || len(ds) != 1 || ds[0].Visibility != store.DepotPrivate {
		t.Fatalf("Depots = %+v, %v; want one private depot", ds, err)
	}
}

// Making a depot public, or back to private, is the operator's alone, as a
// change of its kind is: a yardmaster's re-put that flips it is refused and
// logs nothing, the operator's passes, and what it logs holds the visibility.
func TestDepotVisibilityChangeIsTheOperators(t *testing.T) {
	t.Parallel()
	api, own := operatorYard(t)
	ctx := context.Background()
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: "/srv/web", Base: "main"}, "jo"); err != nil {
		t.Fatal(err)
	}
	const does = "change a depot's visibility"
	public := store.Depot{Name: "web", Path: "/srv/web", Base: "main", Visibility: store.DepotPublic}
	head, err := api.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := api.PutDepot(ctx, public, "yardmaster"); err == nil ||
		err.Error() != "only the operator (jo) may "+does+"; this is yardmaster" {
		t.Fatalf("public as yardmaster = %v", err)
	}
	if err := own.PutDepot(ctx, public, "yardmaster"); err == nil {
		t.Fatal("public as yardmaster through own = nil")
	} else if ask, ok := store.IsOperatorOnly(err); !ok || ask != store.Alone {
		t.Fatalf("public as yardmaster = %v (mark %v, %v), want an operator-only refusal marked alone", err, ask, ok)
	}
	if now, _ := api.Head(ctx); now != head {
		t.Fatalf("the refusal logged %d events", now-head)
	}
	if d, _ := api.GetDepot(ctx, "web"); d.Visibility != store.DepotPrivate {
		t.Fatalf("a refused change changed: %+v", d)
	}
	if err := api.PutDepot(ctx, public, "jo"); err != nil {
		t.Fatalf("public as the operator: %v", err)
	}
	if d, _ := api.GetDepot(ctx, "web"); d.Visibility != store.DepotPublic {
		t.Fatalf("after the operator's change = %+v, want public", d)
	}
	evs, err := api.EventsOfKind(ctx, "depot_updated", 0)
	if err != nil || len(evs) == 0 {
		t.Fatalf("depot_updated events = %d, %v", len(evs), err)
	}
	if got := evs[len(evs)-1].Data["visibility"]; got != store.DepotPublic {
		t.Fatalf("the last depot_updated says visibility %v, want public", got)
	}
}

// TellDepotVisibility puts the mismatch in the yardmaster's inbox and changes
// no depot: the recorded value is the operator's to set.
func TestTellDepotVisibilityReachesTheYardmasterInbox(t *testing.T) {
	t.Parallel()
	api, _ := operatorYard(t)
	ctx := context.Background()
	if err := api.PutDepot(ctx, store.Depot{Name: "web", Path: "/srv/web"}, "jo"); err != nil {
		t.Fatal(err)
	}
	if err := api.TellDepotVisibility(ctx, "web", store.DepotPrivate, store.DepotPublic); err != nil {
		t.Fatal(err)
	}
	if d, _ := api.GetDepot(ctx, "web"); d.Visibility != store.DepotPrivate {
		t.Fatalf("telling changed the depot: %+v", d)
	}
	page, err := api.Inbox(ctx, "yardmaster", 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []store.Event
	for _, e := range page.Events {
		if e.Kind == store.DepotVisibilityEvent {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("the yardmaster's inbox holds %d depot_visibility events, want 1: %+v", len(got), page.Events)
	}
	e := got[0]
	if e.Data["depot"] != "web" || e.Data["recorded"] != store.DepotPrivate || e.Data["actual"] != store.DepotPublic {
		t.Fatalf("the event names %+v, want depot web, recorded private, actual public", e.Data)
	}
}
