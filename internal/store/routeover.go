package store

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// GroupAfter is how long a bead waits in its stage before a peer group
// that comes after a full group takes it (Route.ChooseOver). Unset is none:
// the peer takes the bead as soon as the groups before it are full or
// passed over.
const GroupAfter = "after"

// PeerAfter is how long a bead waits in its stage before the peer group
// takes it past a full group: 0 when the group sets none (GroupAfter).
func (g Group) PeerAfter() time.Duration {
	d, err := ParseSpan(setting(g.Config[GroupAfter]))
	if err != nil {
		return 0
	}
	return d
}

// Overflow is a route's groups as the dispatcher knows them for a choice to
// pass a full group over to a peer group. The zero value is a route with
// none, which chooses as ChooseAt always did.
type Overflow struct {
	// Full are the groups that could take the bead but for their limit
	// ("at its limit") or for the yard's max_sessions ("capped"), by name.
	// A full group is passed over only toward a peer group later in the
	// route; with none, it is chosen as a group that waits.
	Full map[string]string
	// Peer are the route's peer groups, by name, with how long each waits
	// in the stage before it takes a bead (its after setting).
	Peer map[string]time.Duration
	// Waited is how long the bead has been in its stage.
	Waited time.Duration
}

// ChooseOver is ChooseAt with the overflow the dispatcher knows of: a full
// group is passed over, with its reason, only toward a peer group later in
// the route, and the bead goes to that peer group alone, so no local group
// between the two takes it in its place (a local spill raises what runs at
// once here, which the limits exist to bound). The peer group takes the
// bead once it has waited its after setting in the stage; before that the
// choice has no group and says why in Wait. A peer group is judged as any
// group is, with no condition: it starts no agent here, and takes the bead
// without adding a local session.
func (r Route) ChooseOver(now time.Time, over Overflow, quota func(group, source string) Quota, model func(group string) string) RouteChoice {
	var c RouteChoice
	gs := r.Groups()
	// spilled says a full group was passed over: from there only a peer
	// group is considered.
	spilled := false
	for i, g := range gs {
		if g.Name == RouteWait {
			return c
		}
		_, peer := over.Peer[g.Name]
		// A full group is passed over only toward a peer group after it. With
		// none, it is judged as it always was: its condition, if it fails,
		// passes it, and one that holds takes the bead to wait for it.
		if reason, full := over.Full[g.Name]; full && slices.ContainsFunc(gs[i+1:], func(o RouteGroup) bool {
			_, ok := over.Peer[o.Name]
			return ok
		}) {
			c.Passed = append(c.Passed, PassedGroup{Group: g.Name, Reason: reason})
			spilled = true
			continue
		}
		if spilled && !peer {
			continue
		}
		// Unset (0) holds nothing, whatever the clock says: a bead's stay
		// may start after the pass's clock in a test that sets one.
		if after := over.Peer[g.Name]; peer && after > 0 && over.Waited < after {
			c.Wait = &PassedGroup{Group: g.Name, Reason: fmt.Sprintf(
				"group %s takes a bead once it has waited %s in its stage (after=%s); this bead has waited %s",
				g.Name, Span(after), Span(after), Span(over.Waited))}
			return c
		}
		limits := noWindowUsedUp
		if g.When != "" {
			// AddRoute stored only what parses; a record that does not is
			// treated as what cannot be evaluated.
			parsed, err := ParseQuota(g.When)
			if err != nil {
				c.Group, c.Unknown = g.Name, err.Error()
				return c
			}
			limits = withProviderWindows(parsed)
		} else if quota == nil {
			c.Group = g.Name
			return c
		}
		// Each source a condition names is asked once, "" being the
		// group's provider.
		asked := map[string]Quota{}
		get := func(source string) Quota {
			q, ok := asked[source]
			if !ok {
				q = quota(g.Name, source)
				asked[source] = q
			}
			return q
		}
		m := ""
		if model != nil {
			m = model(g.Name)
		}
		failed, unknown, held, resets := judge(now, limits, get, m)
		if len(failed) == 0 {
			c.Group, c.Held = g.Name, held
			if len(unknown) > 0 {
				c.Unknown = "the limits of " + g.Name + " were not known: " + strings.Join(unknown, "; ")
			}
			return c
		}
		c.Passed = append(c.Passed, PassedGroup{Group: g.Name, Reason: strings.Join(failed, "; "), ResetsAt: resets})
	}
	return c
}
