package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"yardr.dev/yardr/internal/store"
)

// groupSetCmd is yardr group set: some settings of a group changed, the
// rest kept, as crew set does for a crew member. group add cannot do that,
// as it writes the whole group: raising a limit with it costs the group
// every --set that is not retyped.
func (a *app) groupSetCmd() *cobra.Command {
	var unset, members []string
	var limit int
	cmd := &cobra.Command{
		Use:   "set <name> [key=value ...]",
		Short: "Change some settings of a group and keep the rest",
		Long: `Changes the settings named and keeps the rest of the group. key=value is a
runner config key, as --set on yardr group add takes it; --unset <key> removes
one. --limit and --members change what the same flags of add set. The runner
is not changed here: another runner reads another config, so that is yardr
group add, which writes the group whole.

It prints each setting changed, as it was and as it is. The value of a key
named like a secret (token, secret, password, key) is not printed.`,
		Example: `  yardr group set webapp/builders model=opus
  yardr group set webapp/builders --limit 3
  yardr group set webapp/builders --limit 0   # pause: nothing new starts; set back to resume
  yardr group set webapp/builders 'args=["--permission-mode","auto"]' --unset idle
  yardr group set approvers --members mckean,yardmaster`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			name := args[0]
			// parseSets would speak of --set, which this command has not.
			for _, kv := range args[1:] {
				if k, _, ok := strings.Cut(kv, "="); !ok || k == "" {
					return fmt.Errorf("%q: want key=value (--unset <key> removes a setting)", kv)
				}
			}
			e := store.GroupEdit{Unset: unset}
			if e.Set, err = store.ParseSets(args[1:]); err != nil {
				return err
			}
			fl := c.Flags()
			if len(e.Set) == 0 && len(unset) == 0 && !fl.Changed("limit") && !fl.Changed("members") {
				return fmt.Errorf("nothing to set: give key=value, --unset <key>, --limit or --members")
			}
			for _, k := range unset {
				if _, both := e.Set[k]; both {
					return fmt.Errorf("%s is both set and unset", k)
				}
			}
			if fl.Changed("limit") {
				e.Limit = &limit
			}
			if fl.Changed("members") {
				e.Members = &members
			}
			// The store holds the group as it would be to what its runner
			// reads (store.ConfigProblems), as it does for group add.
			ch, err := st.SetGroup(ctx, name, e, a.who())
			if err != nil {
				return bareSetHint(err)
			}
			changes := groupChanges(ch.Was, ch.Is)
			return a.out(c.OutOrStdout(), ch.Is, func(w io.Writer) {
				if len(changes) == 0 {
					fmt.Fprintf(w, "group %s unchanged: it is set so already\n", name)
					return
				}
				fmt.Fprintf(w, "group %s saved\n%s\n", name, changeLines(changes))
			})
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&unset, "unset", nil, "remove this runner config key (repeatable)")
	f.IntVar(&limit, "limit", 0, "max concurrent sessions (0 pauses the group); for a peer group, how many beads may be at that yard at once")
	f.StringSliceVar(&members, "members", nil, "manual groups: actors who may move a bead out of its stages; replaces the list, so name every member wanted (empty: anyone)")
	return cmd
}

// groupChanges lists what a group set changed, the fields first and the
// config keys by name, as defChanges does for a crew member.
func groupChanges(was, is store.Group) []defChange {
	var out []defChange
	if was.Limit != is.Limit {
		out = append(out, defChange{"--limit", strconv.Itoa(was.Limit), strconv.Itoa(is.Limit)})
	}
	if a, b := strings.Join(was.Members, ","), strings.Join(is.Members, ","); a != b {
		out = append(out, defChange{"--members", a, b})
	}
	return append(out, configChanges(was.Config, is.Config)...)
}

func fmtConfig(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + show(m[k])
	}
	return strings.Join(parts, " ")
}

// fmtSets prints a group's config as --set would take it back: a string as
// it is, anything else as JSON, and a string that --set would read as JSON
// (the command "true", the model "5") as the JSON string. So cmd=true is the
// boolean and cmd="true" the command, which fmtConfig prints alike.
func fmtSets(m map[string]any) string {
	parts := make([]string, 0, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		if s, ok := v.(string); ok && !json.Valid([]byte(s)) {
			parts = append(parts, k+"="+s)
			continue
		}
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false) // a command's & and < as they were typed
		if err := enc.Encode(v); err != nil {
			parts = append(parts, k+"="+show(v))
			continue
		}
		parts = append(parts, k+"="+strings.TrimSuffix(b.String(), "\n"))
	}
	return strings.Join(parts, " ")
}

// show prints a decoded JSON value for humans. JSON numbers arrive as
// float64, which %v prints as 2.366465e+06; whole numbers print as integers.
func show(v any) string {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case map[string]any:
		return "{" + fmtConfig(x) + "}"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = show(e)
		}
		return "[" + strings.Join(parts, " ") + "]"
	}
	return fmt.Sprint(v)
}
