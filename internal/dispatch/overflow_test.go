package dispatch_test

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/yardkey"
)

// overflowSetup is setup with the herdr runner standing in for the groups
// that start agents here (a route's further groups must start agents) and
// the peer runner standing for another yard's work, with a link to the
// peer north that carries beads out. It returns the store, the local runner
// and the peer runner's fake.
func overflowSetup(t *testing.T) (store.API, *fakeRunner, *fakeRunner, *dispatch.Dispatcher) {
	t.Helper()
	s, local, d := setup(t)
	peer := &fakeRunner{dead: map[string]bool{}}
	d.Runners["herdr"] = local
	d.Runners[store.PeerRunner] = peer
	ctx := context.Background()
	k, _, err := yardkey.Ensure(filepath.Join(t.TempDir(), "north.key"))
	must(t, err)
	_, err = s.AddPeer(ctx, store.Peer{Name: "north", Key: store.Public(k)}, "me")
	must(t, err)
	_, err = s.AllowPeer(ctx, "north", store.PeerAllow{Send: []string{store.PeerBead}}, "me")
	must(t, err)
	return s, local, peer, d
}

// routeNew is the route to new that names its further groups in order.
func routeNew(t *testing.T, s store.API, group string, further ...store.RouteGroup) {
	t.Helper()
	_, err := s.AddRoute(context.Background(), store.Route{Stage: "new", Group: group, Else: further}, "me")
	must(t, err)
}

// claimedPassed is the claimed event's passed list on bead id: each passed
// group's name and reason, in order.
func claimedPassed(t *testing.T, s store.API, id string) (group string, passed [][2]string) {
	t.Helper()
	evs, err := s.Events(context.Background(), id, 0)
	must(t, err)
	for _, e := range evs {
		if e.Kind != "claimed" {
			continue
		}
		group, _ = e.Data["group"].(string)
		list, _ := e.Data["passed"].([]any)
		for _, p := range list {
			m, _ := p.(map[string]any)
			g, _ := m["group"].(string)
			r, _ := m["reason"].(string)
			passed = append(passed, [2]string{g, r})
		}
	}
	return group, passed
}

// groupWaits are the group_wait events on bead id.
func groupWaits(t *testing.T, s store.API, id string) []store.Event {
	t.Helper()
	evs, err := s.Events(context.Background(), id, 0)
	must(t, err)
	return slices.DeleteFunc(evs, func(e store.Event) bool { return e.Kind != "group_wait" })
}

// A full local group is waited for when no peer group follows it: the
// bead waits with one group_wait, and the group after it, which is free,
// takes nothing (overflow v2 changes nothing for a herdr chain).
func TestOverflowLocalFullStillWaits(t *testing.T) {
	t.Parallel()
	s, local, _, d := overflowSetup(t)
	ctx := context.Background()
	must(t, s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 1}, "me"))
	must(t, s.PutGroup(ctx, store.Group{Name: "reserve", Runner: "herdr", Limit: 1}, "me"))
	routeNew(t, s, "builders", store.RouteGroup{Name: "reserve"})

	b1, _ := s.CreateBead(ctx, store.NewBead{Title: "first"}, "me")
	b2, _ := s.CreateBead(ctx, store.NewBead{Title: "second"}, "me")
	for range 3 {
		must(t, d.Reconcile(ctx))
	}
	if got := local.starts(); !slices.Equal(got, []string{b1.ID}) {
		t.Fatalf("starts = %v, want only %s: the reserve group takes nothing past a full builders", got, b1.ID)
	}
	if n := len(groupWaits(t, s, b2.ID)); n != 1 {
		t.Fatalf("group_wait events on the waiting bead after 3 passes = %d, want 1", n)
	}
}

// A bead its local group is full for goes to the peer group after it, which
// takes it without a session here, and the claim says why it passed over
// the local group: at its limit.
func TestOverflowLocalFullGoesToPeer(t *testing.T) {
	t.Parallel()
	s, local, peer, d := overflowSetup(t)
	ctx := context.Background()
	must(t, s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 1}, "me"))
	must(t, s.PutGroup(ctx, store.Group{Name: "north-builders", Runner: store.PeerRunner, Limit: 2,
		Config: map[string]any{store.GroupPeer: "north"}}, "me"))
	routeNew(t, s, "builders", store.RouteGroup{Name: "north-builders"})

	b1, _ := s.CreateBead(ctx, store.NewBead{Title: "first"}, "me")
	b2, _ := s.CreateBead(ctx, store.NewBead{Title: "second"}, "me")
	for range 3 {
		must(t, d.Reconcile(ctx))
	}
	if got := local.starts(); !slices.Equal(got, []string{b1.ID}) {
		t.Fatalf("local starts = %v, want %s only", got, b1.ID)
	}
	group, passed := claimedPassed(t, s, b2.ID)
	if group != "north-builders" || !slices.Equal(passed[0][:], []string{"builders", "at its limit"}) || len(passed) != 1 {
		t.Fatalf("claim of the overflowed bead: group %q passed %v, want north-builders past builders (at its limit)", group, passed)
	}
	if got := peer.starts(); !slices.Equal(got, []string{b2.ID}) {
		t.Fatalf("peer starts = %v, want %s", got, b2.ID)
	}
}

// max_sessions holds the yard's local group as its limit does: a bead that
// the cap holds goes to the peer group after it, and the claim says capped.
func TestOverflowCappedGoesToPeer(t *testing.T) {
	t.Parallel()
	s, local, peer, d := overflowSetup(t)
	ctx := context.Background()
	must(t, s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 3}, "me"))
	must(t, s.PutGroup(ctx, store.Group{Name: "north-builders", Runner: store.PeerRunner, Limit: 2,
		Config: map[string]any{store.GroupPeer: "north"}}, "me"))
	routeNew(t, s, "builders", store.RouteGroup{Name: "north-builders"})
	must(t, s.SetSetting(ctx, store.MaxSessionsSetting, "1", "me"))

	b1, _ := s.CreateBead(ctx, store.NewBead{Title: "first"}, "me")
	b2, _ := s.CreateBead(ctx, store.NewBead{Title: "second"}, "me")
	for range 3 {
		must(t, d.Reconcile(ctx))
	}
	if got := local.starts(); !slices.Equal(got, []string{b1.ID}) {
		t.Fatalf("local starts under max_sessions 1 = %v, want %s only", got, b1.ID)
	}
	group, passed := claimedPassed(t, s, b2.ID)
	if group != "north-builders" || len(passed) != 1 || !slices.Equal(passed[0][:], []string{"builders", "capped"}) {
		t.Fatalf("claim of the capped bead: group %q passed %v, want north-builders past builders (capped)", group, passed)
	}
	if got := peer.starts(); !slices.Equal(got, []string{b2.ID}) {
		t.Fatalf("peer starts = %v, want %s", got, b2.ID)
	}
}

// A full peer group waits as any group does: the bead it cannot take stays
// waiting, with one group_wait that names the peer.
func TestOverflowFullPeerWaits(t *testing.T) {
	t.Parallel()
	s, _, peer, d := overflowSetup(t)
	ctx := context.Background()
	must(t, s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 1}, "me"))
	must(t, s.PutGroup(ctx, store.Group{Name: "north-builders", Runner: store.PeerRunner, Limit: 1,
		Config: map[string]any{store.GroupPeer: "north"}}, "me"))
	routeNew(t, s, "builders", store.RouteGroup{Name: "north-builders"})

	s.CreateBead(ctx, store.NewBead{Title: "first"}, "me")
	b2, _ := s.CreateBead(ctx, store.NewBead{Title: "second"}, "me")
	b3, _ := s.CreateBead(ctx, store.NewBead{Title: "third"}, "me")
	for range 4 {
		must(t, d.Reconcile(ctx))
	}
	if got := peer.starts(); !slices.Equal(got, []string{b2.ID}) {
		t.Fatalf("peer starts = %v, want %s only: its limit is 1", got, b2.ID)
	}
	waits := groupWaits(t, s, b3.ID)
	if len(waits) != 1 {
		t.Fatalf("group_wait events on the third bead = %d, want 1", len(waits))
	}
	if reason, _ := waits[0].Data["reason"].(string); !containsAll(reason, "group north-builders", "its limit (1)") {
		t.Fatalf("group_wait reason = %q, want the peer group's limit", reason)
	}
}

// A peer group's after setting holds the bead until it has waited that
// long in its stage: at 14 minutes it waits, at 16 it goes to the peer.
func TestOverflowAfterWaitsThenGoes(t *testing.T) {
	t.Parallel()
	s, _, peer, d := overflowSetup(t)
	ctx := context.Background()
	must(t, s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr", Limit: 1}, "me"))
	must(t, s.PutGroup(ctx, store.Group{Name: "north-builders", Runner: store.PeerRunner, Limit: 2,
		Config: map[string]any{store.GroupPeer: "north", store.GroupAfter: "15m"}}, "me"))
	routeNew(t, s, "builders", store.RouteGroup{Name: "north-builders"})

	s.CreateBead(ctx, store.NewBead{Title: "first"}, "me")
	b2, _ := s.CreateBead(ctx, store.NewBead{Title: "second"}, "me")
	created := time.Now()
	dispatch.SetClock(d, func() time.Time { return created.Add(14 * time.Minute) })
	for range 2 {
		must(t, d.Reconcile(ctx))
	}
	if got := peer.starts(); len(got) != 0 {
		t.Fatalf("peer started at 14m with after=15m: %v", got)
	}
	if n := len(groupWaits(t, s, b2.ID)); n != 1 {
		t.Fatalf("group_wait events at 14m = %d, want 1", n)
	}
	dispatch.SetClock(d, func() time.Time { return created.Add(16 * time.Minute) })
	must(t, d.Reconcile(ctx))
	if got := peer.starts(); !slices.Equal(got, []string{b2.ID}) {
		t.Fatalf("peer starts at 16m = %v, want %s", got, b2.ID)
	}
}

// containsAll is whether s holds every part.
func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
