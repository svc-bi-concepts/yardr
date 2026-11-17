package store

import (
	"context"
	"slices"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

// ReadyBead is a ready bead no group owns yet, as much of it as says where
// it is routed and whether it can start now: the dispatcher asks for all of
// them on every pass and starts few, so it reads a bead (GetBead) when it
// starts it. The handle is under "bead", not "id", so a daemon and a server
// of different builds read no bead in each other's answer instead of
// starting one without its body.
type ReadyBead struct {
	Bead   string   `json:"bead"`
	Stage  string   `json:"stage"`
	Type   string   `json:"type"`
	Depot  *string  `json:"depot,omitempty"`
	Labels []string `json:"labels,omitempty"`
	// Model is the model the bead pins (metadata model=<name>), or "".
	Model string `json:"model,omitempty"`
	// Priority is the bead's, which a route may ask for (Route.BeadPriority).
	Priority int `json:"priority"`
	// Originator is whose subscription the bead builds on, which a route
	// may ask for. Empty reads as the operator's, as on the bead.
	Originator string `json:"originator,omitempty"`
	// StayStart is when the bead's current stay in its stage started, which
	// a peer group's after setting counts from (ChooseOver). Zero from a
	// server that predates it, which reads as waited long enough.
	StayStart time.Time `json:"stay_start,omitempty"`
}

// Dispatchable lists ready beads that no group owns yet, by priority, then
// stage-entry time: within a priority the bead that entered its stage
// first goes first, so a young bead waits behind no older bead that
// reached the stage later. The sort stays stable over openBeads'
// creation order, and runs on the rows. ReadyBead carries the stay's start
// only for the peer groups' after setting, which reads it from the row.
func (s *Store) Dispatchable(ctx context.Context) ([]ReadyBead, error) {
	out := []ReadyBead{}
	err := s.withUntilRepos(ctx, func(look *untilLooks) error {
		out = []ReadyBead{}
		return s.read(func(tx *bolt.Tx) error {
			rows, err := s.openBeads(tx)
			if err != nil {
				return err
			}
			var cands []*beadRow
			for _, r := range rows {
				if r.Group != nil || !ready(tx, &r.Bead, look) {
					continue
				}
				cands = append(cands, r)
			}
			sort.SliceStable(cands, func(i, j int) bool {
				if cands[i].Priority != cands[j].Priority {
					return cands[i].Priority < cands[j].Priority
				}
				return cands[i].StayStart.Before(cands[j].StayStart)
			})
			for _, r := range cands {
				model, _ := r.Metadata["model"].(string)
				out = append(out, ReadyBead{Bead: r.ID, Stage: r.Stage, Type: r.Type, Depot: copyOf(r.Depot),
					Labels: slices.Clone(r.Labels), Model: model, Priority: r.Priority, Originator: r.Originator,
					StayStart: r.StayStart})
			}
			return nil
		})
	})
	return out, err
}
