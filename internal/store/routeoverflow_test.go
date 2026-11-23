package store_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/yardkey"
)

// A full group is passed over only toward a peer group after it, and the
// bead goes to that peer group alone; a peer group takes the bead once the
// bead has waited its after setting in the stage. The rows are the route's
// groups in order, the dispatcher's evidence and how long the bead waited.
func TestChooseOverPassesFullGroupToPeerGroup(t *testing.T) {
	t.Parallel()
	route := func(names ...string) store.Route {
		r := store.Route{Group: names[0]}
		for _, n := range names[1:] {
			r.Else = append(r.Else, store.RouteGroup{Name: n})
		}
		return r
	}
	peer := func(after time.Duration) map[string]time.Duration {
		return map[string]time.Duration{"p": after}
	}
	full := func(reason string, groups ...string) map[string]string {
		m := map[string]string{}
		for _, g := range groups {
			m[g] = reason
		}
		return m
	}
	passed := func(group, reason string) []store.PassedGroup {
		return []store.PassedGroup{{Group: group, Reason: reason}}
	}
	for name, tc := range map[string]struct {
		route store.Route
		over  store.Overflow
		want  store.RouteChoice
	}{
		"no evidence: the first group is taken, as before": {
			route("a", "p"), store.Overflow{}, store.RouteChoice{Group: "a"}},
		"a full group with no peer after it is waited for, as before": {
			route("a", "b"), store.Overflow{Full: full("at its limit", "a")}, store.RouteChoice{Group: "a"}},
		"a full local group goes to the peer group after it": {
			route("a", "p"), store.Overflow{Full: full("at its limit", "a"), Peer: peer(0)},
			store.RouteChoice{Group: "p", Passed: passed("a", "at its limit")}},
		"a capped local group goes to the peer group after it": {
			route("a", "p"), store.Overflow{Full: full("capped", "a"), Peer: peer(0)},
			store.RouteChoice{Group: "p", Passed: passed("a", "capped")}},
		"no local group between a full one and the peer takes the bead": {
			route("a", "b", "p"), store.Overflow{Full: full("capped", "a"), Peer: peer(0)},
			store.RouteChoice{Group: "p", Passed: passed("a", "capped")}},
		"a full peer group is chosen, and its limit is what waits": {
			route("a", "p"), store.Overflow{Full: full("at its limit", "a", "p"), Peer: peer(0)},
			store.RouteChoice{Group: "p", Passed: passed("a", "at its limit")}},
		"a peer group after=15m that the bead has waited 14m for waits": {
			route("a", "p"), store.Overflow{Full: full("at its limit", "a"), Peer: peer(15 * time.Minute), Waited: 14 * time.Minute},
			store.RouteChoice{Passed: passed("a", "at its limit"),
				Wait: &store.PassedGroup{Group: "p", Reason: "group p takes a bead once it has waited 15m in its stage (after=15m); this bead has waited 14m"}}},
		"a peer group after=15m that the bead has waited 16m for goes": {
			route("a", "p"), store.Overflow{Full: full("at its limit", "a"), Peer: peer(15 * time.Minute), Waited: 16 * time.Minute},
			store.RouteChoice{Group: "p", Passed: passed("a", "at its limit")}},
		"a peer group alone waits out its after setting": {
			route("p"), store.Overflow{Peer: peer(15 * time.Minute)},
			store.RouteChoice{Wait: &store.PassedGroup{Group: "p", Reason: "group p takes a bead once it has waited 15m in its stage (after=15m); this bead has waited 0s"}}},
	} {
		got := tc.route.ChooseOver(time.Now(), tc.over, nil, nil)
		if got.Wait != nil && tc.want.Wait != nil {
			// The words are written where the reason is made; here the peer
			// that waits and that its reason names its after setting.
			if got.Wait.Group != tc.want.Wait.Group || !strings.Contains(got.Wait.Reason, "after=") {
				t.Errorf("%s: wait = %+v, want %+v", name, got.Wait, tc.want.Wait)
			}
			got.Wait, tc.want.Wait = nil, nil
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", name, got, tc.want)
		}
	}
}

// A peer group is a route's last group, and takes no condition: the route
// is refused for each other shape, and accepted with a peer group last.
func TestRouteRefusesPeerGroupOutOfPlace(t *testing.T) {
	t.Parallel()
	api := storetest.New(t)
	ctx := context.Background()
	k, _, err := yardkey.Ensure(filepath.Join(t.TempDir(), "north.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := api.AddPeer(ctx, store.Peer{Name: "north", Key: store.Public(k)}, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.AllowPeer(ctx, "north", store.PeerAllow{Send: []string{store.PeerBead}}, "me"); err != nil {
		t.Fatal(err)
	}
	put := func(name, runner string, cfg map[string]any) {
		t.Helper()
		if err := api.PutGroup(ctx, store.Group{Name: name, Runner: runner, Limit: 2, Config: cfg}, "me"); err != nil {
			t.Fatal(err)
		}
	}
	put("builders", "herdr", nil)
	put("reserve", "herdr", nil)
	put("north-builders", store.PeerRunner, map[string]any{store.GroupPeer: "north"})

	for name, tc := range map[string]struct {
		route store.Route
		ok    bool
		want  string
	}{
		"a herdr group else a peer group last": {
			store.Route{Stage: "new", Group: "builders", Else: []store.RouteGroup{{Name: "north-builders"}}}, true, ""},
		"a peer group alone": {
			store.Route{Stage: "new", Group: "north-builders"}, true, ""},
		"a peer group in the middle": {
			store.Route{Stage: "new", Group: "north-builders", Else: []store.RouteGroup{{Name: "reserve"}}}, false, "group north-builders is a peer group: it must be the route's last group"},
		"a condition on a peer group": {
			store.Route{Stage: "new", Group: "builders", Else: []store.RouteGroup{{Name: "north-builders", When: "Weekly<80"}}}, false, "group north-builders is a peer group: it takes no condition"},
		"--else wait after a peer group": {
			store.Route{Stage: "new", Group: "builders", Else: []store.RouteGroup{{Name: "north-builders"}, {Name: store.RouteWait}}}, false, "--else wait after peer group north-builders"},
	} {
		r, err := api.AddRoute(ctx, tc.route, "me")
		switch {
		case tc.ok && err != nil:
			t.Errorf("%s: refused: %v", name, err)
		case !tc.ok && err == nil:
			t.Errorf("%s: accepted, want refused", name)
		case !tc.ok && !strings.Contains(err.Error(), tc.want):
			t.Errorf("%s: refused with %q, want it to say %q", name, err, tc.want)
		}
		if err == nil {
			if err := api.DeleteRoute(ctx, r.ID, "me"); err != nil {
				t.Fatal(err)
			}
		}
	}
}
