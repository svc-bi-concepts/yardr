package cli

import (
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"strings"
	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/store"
)

func (a *app) routeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "route", Short: "Which group works which stage"}
	var r store.Route
	var typ, depot, group, originator string
	var labels, further []string
	var beadPriority int
	add := &cobra.Command{
		Use:   "add --stage <stage> --group <group>[@<condition>] [--else <group>[@<condition>]]...",
		Short: "Send open, unblocked beads in a stage to a group",
		Long: `Send open, unblocked beads in a stage to a group.

A route may name further groups with --else, in the order they are tried,
and a group that starts agents may carry a condition after @ on the windows
of its provider's usage limits, as the yard's limits source labels them
(yardr limits): Weekly<80 holds while the window labelled Weekly is under
80% used, Weekly<80,5h<90 while both are, <80 while every window is. A
comparison may name a source for the window: machine.load<80 holds while
the window labelled load of what the source machine says of itself is under
80% used (yardr limits names such a source beside the providers), whatever
the group's provider is, and the two forms mix (machine.load<80,Weekly<90).
In place of a number the right side may be the window's target, the share of
it that has passed as the source reports it, with an optional margin:
Weekly<target holds while Weekly is under its target, Weekly<target+10
while under ten points above it, Weekly<target-5 while under five below,
and the two forms mix (Weekly<target+10,5h<90). Any comparison may end in
/<duration>, days (1d) or hours and minutes (20h, 30m): Weekly<80/1d holds
while Weekly is under 80% used or resets within a day, so quota that is
about to renew does not keep a bead waiting. It goes on the target form
(Weekly<target+10/1d) and on a source's window too, though a window that
never resets, as the machine source's, holds nothing by it. A reset
already past counts; a window whose report gives no reset is held to the
number alone. The claimed event says which windows held (held). A bead
goes to the first group whose condition holds; a group that starts agents
with none, or with one on sources alone, holds while no window of its
provider is used up (as <100 would; a window the source says counts for one
model alone counts only where it is the model the bead would start with in
that group: yardr limits marks it), and --else wait ends the route, so the
bead waits. While no group's does, the bead waits (a quota_wait event says
for what). A condition that cannot be evaluated (no limits source answers
for the group's provider, its command failed, it reports no such window, it
gives the window no percentage or, for a condition on the target, no
target, and for a comparison on a source no source of the name that answers
for itself) holds, and the claimed event says the limits were not known.

Two groups of a route that burn one provider on one login read the same
windows, so a further group with no condition that burns the provider of a
group before it takes the beads that group was passed over for, until those
windows are used up: route add warns of it and adds the route. Two groups
on different logins read each
their own, and a personal group failing over to the operator's warns of
nothing. A route with --else or a condition otherwise names only
groups that start agents, or gates one exec group alone on the windows of
sources, as refinery@machine.load<80.

A peer group, one whose runner is peer (another yard works its beads), is the
route's last group: it takes no condition, and no --else wait follows it. A
group at its limit, or a bead held by max_sessions, is passed over only
toward such a peer group, and no local group between the two takes the bead
in its place: a peer takes it without a session here. A route with no peer
group waits at a full group as before. The peer group takes the bead once it
has waited its after setting in the stage (yardr group set <name> after=15m;
unset: at once), and its own limit still holds. yardr bead show says which route gave
a bead to its group and which groups it passed over, or what a waiting bead
waits for.

--type, --depot, --label and --bead-priority say which beads of the stage
the route is for; one it does not give matches any bead. --label may be
given more than once, and the bead must have every one. --bead-priority
<n> matches beads of priority n or more urgent: 1 is P0 and P1.
--originator <user> takes only that user's beads, and --originator any
every originator's; a route without it takes only the operator's beads, so
no general route swallows another person's work, unless its first group
starts no agents (a script, people, another yard): none burns a
subscription, so that route is any. A named originator must be a user that
exists. Without --depot a route is a yard default: it serves
every depot that does not state its own route for the same beads (the
same stage, type, labels, bead priority and originator); with --depot it
is that depot's own, and sets a default aside only for those beads and
only there. A default may name its group with the depot template, first
group or further group: '{depot}-assembly' reads as 'web-assembly' for
depot web, and a depot without the resolved group keeps no route from it.
An own route names no template. Of the routes that match a bead, in its
depot's effective routes, the first in this order takes it, the first of
these that differs deciding: urgency (a route with --bead-priority before
one without, and the narrower threshold first: 0 before 1); asked for by
name (more labels that are not estimates, as claude or airy); estimate
(more labels that begin difficulty: or size:); the rest (the type, the depot
and a named originator count one each); an own route before a yard default;
the route id. A route that takes the same beads as one of the same scope
already standing is refused, naming it: the two would be ordered by id,
which nobody chose. A --note says in words what the filters and the chain
cannot: why this route is there. route list prints it.`,
		Example: `  yardr route add --stage new --group webapp/builders --depot webapp
  yardr route add --stage review --group webapp/reviewers
  yardr route add --stage new --type video --group herald
  yardr route add --stage new --group 'builders@Weekly<80,5h<90' --else spare-builders
  yardr route add --stage new --group 'builders@Weekly<target+10' --else spare-builders
  yardr route add --stage new --group 'builders@Weekly<80/1d' --else spare-builders
  yardr route add --stage new --group 'builders@machine.load<80' --else wait
  yardr route add --stage new --label difficulty:3 --label size:large --group builders
  yardr route add --stage new --bead-priority 0 --group builders --note 'P0 reserve'
  yardr route add --stage new --depot web --originator mckean2 --group mckean2-builders
  yardr route add --stage approved --group '{depot}-assembly'`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if r.Stage == "" || group == "" {
				return fmt.Errorf("--stage and --group are required")
			}
			first := store.ParseRouteGroup(group)
			r.Group, r.When, r.Else = first.Name, first.When, nil
			for _, e := range further {
				r.Else = append(r.Else, store.ParseRouteGroup(e))
			}
			fl := c.Flags()
			if fl.Changed("type") {
				r.Type = &typ
			}
			if fl.Changed("depot") {
				r.Depot = &depot
			}
			// AddRoute stores the first label as the route's label and the
			// rest beside it.
			r.Label, r.AndLabels, r.BeadPriority, r.Originator = nil, labels, nil, nil
			if fl.Changed("bead-priority") {
				r.BeadPriority = &beadPriority
			}
			if fl.Changed("originator") {
				r.Originator = &originator
			}
			st, err := a.store(c.Context())
			if err != nil {
				return err
			}
			saved, err := st.AddRoute(c.Context(), r, a.who())
			if err != nil {
				return err
			}
			// The route is the operator's to write: what cannot work as it
			// reads is said, and the route stands.
			for _, w := range routeWarnings(c.Context(), st, *saved) {
				fmt.Fprintf(c.ErrOrStderr(), "warning: route %s: %s\n", saved.ID, w)
			}
			return a.out(c.OutOrStdout(), saved, func(w io.Writer) { fmt.Fprintln(w, saved.ID) })
		},
	}
	f := add.Flags()
	f.StringVarP(&r.Stage, "stage", "s", "", "stage to match")
	f.StringVarP(&group, "group", "g", "", "group to send to, with an optional quota condition: 'builders@Weekly<80'")
	f.StringArrayVar(&further, "else", nil, "a further group, tried when the ones before it are passed over; repeatable, in order")
	f.StringVarP(&typ, "type", "t", "", "only beads of this type")
	f.StringVarP(&depot, "depot", "d", "", "only beads in this depot")
	f.StringArrayVarP(&labels, "label", "l", nil, "only beads with this label; repeatable, and the bead must have every one")
	f.IntVar(&beadPriority, "bead-priority", 0, "only beads of this priority or more urgent: 1 is P0 and P1")
	f.StringVar(&originator, "originator", "", "only beads for this user, or any for every originator's; unset: the operator's, or any when the first group starts no agents")
	f.StringVar(&r.Note, "note", "", "why this route is there, in words; route list prints it")

	var listDepot string
	list := &cobra.Command{
		Use: "list", Short: "List routes in match order", Args: cobra.NoArgs,
		Long: `List the yard's routes in match order: the scope (the depot's own, or a
yard default), what each takes, its groups and its note.
--depot <depot> lists that depot's effective routes instead: its own routes
plus the defaults none of them sets aside, each marked own or default, with
the defaults it sets aside and the defaults that do not serve it for a
missing group named underneath.`,
		RunE: func(c *cobra.Command, _ []string) error {
			st, err := a.store(c.Context())
			if err != nil {
				return err
			}
			settings, err := st.Settings(c.Context())
			if err != nil {
				return err
			}
			operator := settings[store.OperatorSetting]
			rs, err := st.Routes(c.Context())
			if err != nil {
				return err
			}
			dispatch.SortRoutes(rs)
			if !c.Flags().Changed("depot") {
				return a.out(c.OutOrStdout(), rs, func(w io.Writer) {
					rows := make([][]string, 0, len(rs))
					for _, r := range rs {
						scope := "default"
						if r.Depot != nil {
							scope = *r.Depot
						}
						rows = append(rows, routeRow(r, scope, operator))
					}
					table(w, "ID\tSCOPE\tSTAGE\tTYPE\tLABEL\tBEADS\tORIGINATOR\tGROUP\tNOTE", rows)
				})
			}
			// As depot show fails for a depot that is not there.
			if _, err := st.GetDepot(c.Context(), listDepot); err != nil {
				return err
			}
			listed, err := st.Groups(c.Context())
			if err != nil {
				return err
			}
			byName := make(map[string]store.Group, len(listed))
			for _, g := range listed {
				byName[g.Name] = g
			}
			eff := store.EffectiveRoutes(rs, &listDepot, operator, byName)
			dispatch.SortRoutes(eff)
			aside := store.SetAsideRoutes(rs, listDepot, operator)
			if aside == nil {
				aside = []store.SetAside{}
			}
			unserved := store.UnservedRoutes(rs, listDepot, operator, byName)
			if unserved == nil {
				unserved = []store.UnservedRoute{}
			}
			marked := make([]markedRoute, 0, len(eff))
			for _, r := range eff {
				mark := "default"
				if r.Depot != nil {
					mark = "own"
				}
				marked = append(marked, markedRoute{Route: r, Effective: mark})
			}
			return a.out(c.OutOrStdout(), map[string]any{"routes": marked, "set_aside": aside, "unserved": unserved}, func(w io.Writer) {
				rows := make([][]string, 0, len(marked))
				for _, m := range marked {
					rows = append(rows, routeRow(m.Route, m.Effective, operator))
				}
				table(w, "ID\tOWN/DEFAULT\tSTAGE\tTYPE\tLABEL\tBEADS\tORIGINATOR\tGROUP\tNOTE", rows)
				saySetAside(w, listDepot, aside, operator)
				sayUnserved(w, listDepot, unserved)
			})
		},
	}
	list.Flags().StringVarP(&listDepot, "depot", "d", "", "list this depot's effective routes: its own plus the defaults none sets aside")
	rm := &cobra.Command{
		Use: "rm <id>", Short: "Delete a route", Args: cobra.ExactArgs(1),
		Long: `Deletes a route. <id> is the whole id, or its first characters when there
are at least ` + fmt.Sprint(store.MinPrefix) + ` of them and only one route's id begins with them. An id
no route has is refused, and a start that fits several routes names them, so
a mistyped id cannot pass for a removal.

Beads already claimed through the route keep its group until their
sessions end; only new claims follow the remaining routes.`,
		Example: `  yardr route rm 57h28dx9dx9cmydnrzj9
  yardr route rm 57h2`,
		RunE: func(c *cobra.Command, args []string) error {
			st, err := a.store(c.Context())
			if err != nil {
				return err
			}
			rs, err := st.Routes(c.Context())
			if err != nil {
				return err
			}
			r, err := routeByRef(rs, args[0])
			if err != nil {
				return err
			}
			if err := st.DeleteRoute(c.Context(), r.ID, a.who()); err != nil {
				return err
			}
			// Beads already claimed through the route stay with its group until
			// their sessions end; only new claims follow the remaining routes.
			ss, err := st.Sessions(c.Context(), false)
			if err != nil {
				return err
			}
			claimed := []string{}
			for _, s := range ss {
				if r.Names(s.Group) && s.Stage == r.Stage {
					claimed = append(claimed, s.Bead)
				}
			}
			return a.out(c.OutOrStdout(), map[string]any{"route": r.ID, "claimed": claimed}, func(w io.Writer) {
				if len(claimed) > 0 {
					fmt.Fprintf(w, "%d bead(s) in %s already claimed by %s keep it until their sessions end: %s\n",
						len(claimed), r.Stage, strings.Join(append([]string{r.Worker()}, r.Further()...), " or "), strings.Join(claimed, " "))
				}
			})
		},
	}
	cmd.AddCommand(add, list, rm, a.routeSetCmd(), a.routeExplainCmd(), a.routeReportCmd())
	return cmd
}
