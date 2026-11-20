package store

import (
	"context"
	"maps"
	"time"

	bolt "go.etcd.io/bbolt"
)

// KindIdentityHeld is the kind of the event a held delivery is logged as
// when its envelope matches a crew member's source subscriptions
// (heldNotice). It is about no bead, and it carries the card, the source,
// the sender, the channel and how many wait, never the message: its text
// stays on the card until the card is answered.
const KindIdentityHeld = "identity_held"

// HeldNoticeGap is how long a card's notice stands: a delivery that makes
// the card grow tells the crew again only once this has passed since the
// last notice, so a stranger who keeps writing is not a stream in anyone's
// inbox.
const HeldNoticeGap = time.Hour

var bHeldTold = []byte("held_identity_told") // card bead UUID: heldTold, the last notice

func init() { allBuckets = append(allBuckets, bHeldTold) }

// heldTold is the last notice a card gave: how many deliveries waited then,
// and when. It is dropped with the last delivery a card holds (putHeld), so
// an answered card leaves nothing behind.
type heldTold struct {
	Held int       `json:"held"`
	At   time.Time `json:"at"`
}

// heldNotice tells the crew of a held delivery on the card uuid, which
// sourceName's sender sent from channel and envelope describes, with count
// deliveries waiting now. The event is routed as a kept delivery is
// (hookDeliveries), so it reaches crew members with matching source
// subscriptions, and no user: the operator finds the card in HeldCards.
// Nothing is written where no crew member is reached, and a card is told
// once, then again only when it has grown since its last notice and
// HeldNoticeGap has passed.
func (w *wtx) heldNotice(uuid, sourceName, sender, channel string, envelope map[string]any, count int) error {
	var last heldTold
	told, err := get(w.Tx, bHeldTold, uuid, &last)
	if err != nil {
		return err
	}
	if told && (count <= last.Held || now().Sub(last.At) < HeldNoticeGap) {
		return nil
	}
	// The envelope is routed as a kept delivery's is (delivered), as the
	// source it came from. Its text is only read here, for the mentions and
	// the fields routing matches, and never copied into the event.
	routing := map[string]any{}
	maps.Copy(routing, envelope)
	routing["source"] = sourceName
	deliveries, _, err := hookDeliveries(w.Tx, routing)
	if err != nil || len(deliveries) == 0 {
		return err
	}
	row := new(beadRow)
	if _, err := get(w.Tx, bBeads, uuid, row); err != nil {
		return err
	}
	data := map[string]any{"card": row.ID, "source": sourceName, "sender": sender, "held": count, "deliveries": deliveries}
	if channel != "" {
		data["channel"] = channel
	}
	if err := w.event("", KindIdentityHeld, "yard", data); err != nil {
		return err
	}
	return put(w.Tx, bHeldTold, uuid, heldTold{Held: count, At: now()})
}

// HeldCard is one open mapping card that holds deliveries, as the operator's
// inbox lists it: the card's handle, and whom its held messages came from.
type HeldCard struct {
	Card    string `json:"card"`
	Source  string `json:"source"`
	Sender  string `json:"sender"`
	Channel string `json:"channel,omitempty"`
	Held    int    `json:"held"`
}

// HeldCards are the open mapping cards that hold deliveries, oldest first,
// for the operator to answer. Only an operator is listed them, and a user
// who is no operator gets none. A card that is put aside still waits, so it
// is listed as an open one is. actor is who reads, as UserInbox's is.
func (s *Store) HeldCards(ctx context.Context, name, actor string) ([]HeldCard, error) {
	out := []HeldCard{}
	err := s.read(func(tx *bolt.Tx) error {
		u, err := getUser(tx, name)
		if err != nil {
			return err
		}
		if _, err := s.mayReadInbox(ctx, tx, name, actor, "read"); err != nil {
			return err
		}
		if !u.IsOperator() {
			return nil
		}
		// Only a card that holds something has a list in bHeld, so the walk
		// is over the waiting cards, not every bead.
		var uuids []string
		if err := tx.Bucket(bHeld).ForEach(func(k, _ []byte) error {
			uuids = append(uuids, string(k))
			return nil
		}); err != nil {
			return err
		}
		for _, uuid := range uuids {
			row := new(beadRow)
			if ok, err := get(tx, bBeads, uuid, row); err != nil {
				return err
			} else if !ok || !row.open() {
				continue
			}
			notes, err := beadNotes(tx, uuid)
			if err != nil {
				return err
			}
			held, err := heldList(tx, uuid)
			if err != nil {
				return err
			}
			for _, r := range ResolveAsks(notes, now()) {
				if r.AskErr != nil || r.Ask.Identity == nil || r.Ask.To != name {
					continue
				}
				if r.State != AskOpen && r.State != AskAside {
					continue
				}
				out = append(out, HeldCard{
					Card: row.ID, Source: r.Ask.Identity.Source, Sender: r.Ask.Identity.Sender,
					Channel: r.Ask.Identity.Channel, Held: len(held),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
