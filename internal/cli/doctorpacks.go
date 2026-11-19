package cli

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/version"
	"yardr.dev/yardr/packs"
)

// stalePackNotes are the imports of embedded packs whose copied files
// differ from what the running binary would copy, one line each with the
// commands that bring them up to date. An embedded pack's roles, stage
// files and scripts are copied at pack apply; installing a new yardr
// leaves those copies as they were. The check reuses pack check's
// per-file plan, and compares embedded sources alone: a directory, URL
// or earlier record is not compared. Notes, not faults.
func stalePackNotes(ctx context.Context, st store.API, cfg *config.Config) ([]string, error) {
	recs, err := st.Packs(ctx)
	if err != nil {
		return nil, err
	}
	yardr := yardrAt(cfg.Home)
	var notes []string
	for _, rec := range recs {
		if !fromEmbedded(rec.Source) || !packs.Carries(rec.Pack) {
			continue
		}
		sub, err := fs.Sub(packs.FS, rec.Pack)
		if err != nil {
			continue
		}
		p, err := pack.Load(sub, packs.Embedded+" in yardr "+version.Describe())
		if err != nil {
			continue
		}
		// The last import's flow, crew, groups and roles, as a check
		// without flags repeats them: what differs is the binary's.
		plan, err := pack.Build(ctx, st, p, pack.Options{Depot: rec.Depot, Home: cfg.Home})
		if err != nil {
			continue
		}
		var behind []string
		for _, o := range plan.Objects {
			if o.Kind == "file" && (o.Action == pack.Change || o.Action == pack.Create) {
				behind = append(behind, o.Name)
			}
		}
		if len(behind) == 0 {
			continue
		}
		names := behind
		if len(names) > 3 {
			names = append(slices.Clone(names[:3]), "...")
		}
		depot := ""
		if rec.Depot != "" {
			depot = " --depot " + rec.Depot
		}
		notes = append(notes, fmt.Sprintf("pack %s in %s: %s behind yardr %s (%s): %s pack check %s%s, then %s pack apply %s%s",
			rec.Pack, inDepot(rec.Depot), plural(len(behind), "file"), version.Describe(), strings.Join(names, ", "), yardr, rec.Pack, depot, yardr, rec.Pack, depot))
	}
	return notes, nil
}
