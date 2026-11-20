package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// talkStranger is a talk message from sender in channel C1 that mentions
// mentions, as the receiver keeps its envelope before any speaker is stamped.
// The body is the words a stranger wrote: no notice may carry them.
func talkStranger(sender string, mentions []any, body string) map[string]any {
	return map[string]any{
		"source": "talk", "type": "message", "action": "create",
		"text": body, "mentions": mentions,
		"fields": map[string]any{"sender": []any{sender}, "channel": []any{"C1"}},
	}
}

// holdStranger holds one delivery from sender on its mapping card, under the
// dedupe key key, and reports whether it waits.
func holdStranger(t *testing.T, y *askYard, sender string, mentions []any, body, key string) {
	t.Helper()
	env := talkStranger(sender, mentions, body)
	held, err := y.HoldIdentity(y.ctx, "talk", "talk", sender, "C1", env, []byte(body), []string{key})
	if err != nil || !held {
		t.Fatalf("HoldIdentity(%s, %q) = %v, %v", sender, body, held, err)
	}
}

// A stranger's delivery that mentions a crew member reaches that member's
// inbox as one identity_held event: no message text in it, and no second
// event for a delivery within the hour. Once the hour has passed and the card
// has grown, the crew is told again. The operator finds the card in HeldCards
// while it waits, and no one who is no operator does.
func TestHeldNoticeReachesMentionedCrew(t *testing.T) {
	// The package clock is pinned, so this test stays sequential.
	y := mappingYard(t)
	if _, err := y.PutCrew(y.ctx, CrewMember{Name: "yardmaster", Runner: "herdr"}, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := y.PutCrew(y.ctx, CrewMember{Name: "scribe", Runner: "herdr"}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := y.SetIdentity(y.ctx, "yardmaster", "talk", Identity{ID: "U-yardmaster"}, "mckean"); err != nil {
		t.Fatal(err)
	}
	// A mention reaches a crew member through its subscription to the source,
	// as a kept delivery's does: a held one is routed as that one would be.
	if err := y.AddSubscription(y.ctx, "yardmaster", Subscription{Source: "talk", Scope: ScopeMentions}, "mckean"); err != nil {
		t.Fatal(err)
	}
	if err := y.SetIdentity(y.ctx, "scribe", "talk", Identity{ID: "U-scribe"}, "mckean"); err != nil {
		t.Fatal(err)
	}
	at := y.now
	pinClock(t, &at)

	mention := []any{"U-yardmaster"}
	const body = "hi @yardmaster, a stranger writes this"
	holdStranger(t, y, "U999", mention, body, "talk:U999:1")

	evs, err := y.EventsOfKind(y.ctx, KindIdentityHeld, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("identity_held events = %d, want 1", len(evs))
	}
	if _, ok := evs[0].Data["deliveries"].(map[string]any)["yardmaster"]; !ok {
		t.Fatalf("the event reaches %v, want yardmaster", evs[0].Data["deliveries"])
	}
	if raw, _ := json.Marshal(evs[0]); strings.Contains(string(raw), "stranger writes") {
		t.Fatalf("the notice carries the message text: %s", raw)
	}
	page, err := y.Inbox(y.ctx, "yardmaster", 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Events[0].Kind != KindIdentityHeld {
		t.Fatalf("yardmaster's inbox = %+v, want the one held notice", page)
	}
	if page, err := y.Inbox(y.ctx, "scribe", 10); err != nil || page.Total != 0 {
		t.Fatalf("scribe, not mentioned, has %+v, %v", page, err)
	}

	// Another delivery from the same stranger within the hour holds, and is
	// not a second notice.
	at = at.Add(10 * time.Minute)
	holdStranger(t, y, "U999", mention, "second message", "talk:U999:2")
	if evs, _ := y.EventsOfKind(y.ctx, KindIdentityHeld, 10); len(evs) != 1 {
		t.Fatalf("identity_held events within the hour = %d, want 1", len(evs))
	}

	// After the hour, a card that has grown is told again, with the count.
	at = at.Add(HeldNoticeGap)
	holdStranger(t, y, "U999", mention, "third message", "talk:U999:3")
	evs, _ = y.EventsOfKind(y.ctx, KindIdentityHeld, 10)
	if len(evs) != 2 || fmt.Sprint(evs[1].Data["held"]) != "3" {
		t.Fatalf("after the hour: %d notices, last %+v; want 2, held 3", len(evs), evs)
	}

	// A stranger no one mentions holds its card but tells nobody, so the
	// crew's inboxes gain nothing; the operator sees the card all the same.
	holdStranger(t, y, "U555", nil, "hello, nobody here", "talk:U555:1")
	if evs, _ := y.EventsOfKind(y.ctx, KindIdentityHeld, 10); len(evs) != 2 {
		t.Fatalf("an unmentioned delivery gained a notice: %d, want 2", len(evs))
	}

	cards, err := y.HeldCards(y.ctx, "mckean", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Sender != "U999" || cards[0].Held != 3 ||
		cards[0].Source != "talk" || cards[0].Channel != "C1" || cards[1].Sender != "U555" || cards[1].Held != 1 {
		t.Fatalf("the operator's held cards = %+v", cards)
	}
	if cards[0].Card == "" {
		t.Fatal("a held card names no handle to answer it by")
	}
	if none, err := y.HeldCards(y.ctx, "ada", "ada"); err != nil || len(none) != 0 {
		t.Fatalf("ada, no operator, is listed %+v, %v", none, err)
	}
}

// A card's notice record ends with the card: once the card is answered and
// what it held is gone, no record of its notice is left, and the next
// stranger's card is told as a first one is.
func TestHeldNoticeRecordEndsWithItsCard(t *testing.T) {
	y := mappingYard(t)
	if _, err := y.PutCrew(y.ctx, CrewMember{Name: "yardmaster", Runner: "herdr"}, "me"); err != nil {
		t.Fatal(err)
	}
	if err := y.SetIdentity(y.ctx, "yardmaster", "talk", Identity{ID: "U-yardmaster"}, "mckean"); err != nil {
		t.Fatal(err)
	}
	// A mention reaches a crew member through its subscription to the source,
	// as a kept delivery's does: a held one is routed as that one would be.
	if err := y.AddSubscription(y.ctx, "yardmaster", Subscription{Source: "talk", Scope: ScopeMentions}, "mckean"); err != nil {
		t.Fatal(err)
	}
	at := y.now
	pinClock(t, &at)
	mention := []any{"U-yardmaster"}
	holdStranger(t, y, "U999", mention, "first", "talk:U999:1")
	if n := heldToldCount(t, y.Store); n != 1 {
		t.Fatalf("notice records = %d, want the one card's", n)
	}

	open := mappingQueue(t, y).Open
	if len(open) != 1 {
		t.Fatalf("open cards = %d, want 1", len(open))
	}
	// Let the sender in once: the card closes and what it held is delivered
	// with it, so the notice's record goes too and the next message is told
	// as a first one is, though the hour has not passed.
	if err := y.AnswerAsk(y.ctx, open[0].Bead, open[0].Ask, pickFor(t, y, open[0].Bead, MappingLetIn), "", "mckean", false); err != nil {
		t.Fatal(err)
	}
	if n := heldToldCount(t, y.Store); n != 0 {
		t.Fatalf("notice records after the card closed = %d, want none", n)
	}
	holdStranger(t, y, "U999", mention, "after the card closed", "talk:U999:2")
	if evs, _ := y.EventsOfKind(y.ctx, KindIdentityHeld, 10); len(evs) != 2 {
		t.Fatalf("notices after the card closed = %d, want 2", len(evs))
	}
}

// heldToldCount is how many cards keep a notice record.
func heldToldCount(t *testing.T, s *Store) int {
	t.Helper()
	n := 0
	if err := s.read(func(tx *bolt.Tx) error {
		return tx.Bucket(bHeldTold).ForEach(func(_, _ []byte) error {
			n++
			return nil
		})
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
