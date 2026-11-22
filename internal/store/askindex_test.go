package store

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// queueRows reads a queue as sorted "bead/ask/state" rows, the form
// scanRows reads the same asks in.
func queueRows(q *AskQueue) []string {
	var out []string
	for _, o := range q.Open {
		out = append(out, fmt.Sprintf("%s/%d/open", o.Bead, o.Ask))
	}
	for _, d := range q.Decided {
		out = append(out, fmt.Sprintf("%s/%d/%s", d.Bead, d.Ask, d.State))
	}
	slices.Sort(out)
	return out
}

// scanRows is the queue as it was read before the index: every bead the
// viewer may list, its notes read and the asks they name resolved. The
// index must give the same rows.
func scanRows(t *testing.T, y *askYard, name, viewer string) []string {
	t.Helper()
	beads, err := y.Store.ListBeads(y.ctx, BeadFilter{All: true, Knowledge: true, Viewer: viewer})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range beads {
		notes, err := y.Store.Notes(y.ctx, b.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range ResolveAsks(notes, now()) {
			if r.AskErr != nil || r.Ask.To != name {
				continue
			}
			state := "open"
			if r.State != AskOpen {
				state = r.State.String()
			}
			out = append(out, fmt.Sprintf("%s/%d/%s", b.ID, r.Note.Seq, state))
		}
	}
	slices.Sort(out)
	return out
}

// userQueue reads name's queue as viewer, or as the yard when viewer is "".
func userQueue(t *testing.T, y *askYard, name, viewer string) []string {
	t.Helper()
	var (
		q   *AskQueue
		err error
	)
	if viewer == "" {
		q, err = y.Store.UserAsks(y.ctx, name, name)
	} else {
		q, err = y.Store.UserAsks(y.ctx, name, name, viewer)
	}
	if err != nil {
		t.Fatal(err)
	}
	return queueRows(q)
}

// queueYard builds asks to mckean in every state a queue shows: open,
// answered, declined, taken back, put aside, on a closed bead, and on a bead
// one viewer may not see. Beside them are asks to someone else, and a bead
// with two asks to mckean. It returns the hidden bead's id.
func queueYard(t *testing.T) (*askYard, string) {
	t.Helper()
	pinClockFixed(t, askNow)
	y := decisionYard(t)
	pinClock(t, &y.now)

	open := y.makeBead(t, NewBead{Title: "open"})
	y.askOn(t, open, wordsAsk("mckean"), "yardmaster")

	answered := y.makeBead(t, NewBead{Title: "answered"})
	seq := y.askOn(t, answered, wordsAsk("mckean"), "yardmaster")
	y.tick(time.Minute)
	y.answerOn(t, answered, Answer{Ask: seq, Words: "go on"}, "mckean")

	declined := y.makeBead(t, NewBead{Title: "declined"})
	seq = y.askOn(t, declined, wordsAsk("mckean"), "yardmaster")
	y.tick(time.Minute)
	y.answerOn(t, declined, Answer{Ask: seq, Words: "not mine", Decline: true}, "mckean")

	taken := y.makeBead(t, NewBead{Title: "taken back"})
	seq = y.askOn(t, taken, wordsAsk("mckean"), "yardmaster")
	y.tick(time.Minute)
	if err := y.Store.TakeBackAsk(y.ctx, taken, seq, "no longer needed", "yardmaster"); err != nil {
		t.Fatal(err)
	}

	aside := y.makeBead(t, NewBead{Title: "put aside"})
	seq = y.askOn(t, aside, wordsAsk("mckean"), "yardmaster")
	y.tick(time.Minute)
	y.answerOn(t, aside, Answer{Ask: seq, Later: true}, "mckean")

	closed := y.makeBead(t, NewBead{Title: "closed"})
	y.askOn(t, closed, wordsAsk("mckean"), "yardmaster")
	if _, err := y.Store.CloseBead(y.ctx, closed, "done", Expect{}, "yardmaster"); err != nil {
		t.Fatal(err)
	}

	hidden := y.makeBead(t, NewBead{Title: "hidden from mckean"})
	y.askOn(t, hidden, wordsAsk("mckean"), "yardmaster")

	elsewhere := y.makeBead(t, NewBead{Title: "for ada"})
	y.askOn(t, elsewhere, wordsAsk("ada"), "yardmaster")

	two := y.makeBead(t, NewBead{Title: "two asks"})
	first := y.askOn(t, two, wordsAsk("mckean"), "yardmaster")
	y.tick(time.Minute)
	y.answerOn(t, two, Answer{Ask: first, Words: "yes"}, "mckean")
	y.askOn(t, two, wordsAsk("mckean"), "yardmaster")
	return y, hidden
}

// Every queue the index gives is the queue the scan of the notes gives: for
// the yard and for a viewer who may not see one bead, and again once a put
// aside has come back by time.
func TestUserAsksIndexMatchesScan(t *testing.T) {
	y, hidden := queueYard(t)
	check := func(when string) {
		t.Helper()
		for _, viewer := range []string{"", "mckean"} {
			got := userQueue(t, y, "mckean", viewer)
			want := scanRows(t, y, "mckean", viewer)
			if !slices.Equal(got, want) {
				t.Fatalf("%s, viewer %q: queue = %v, the scan gives %v", when, viewer, got, want)
			}
		}
	}
	check("before any hiding")
	if len(userQueue(t, y, "mckean", "")) < 6 {
		t.Fatalf("the yard's queue = %v, want the asks of every state", userQueue(t, y, "mckean", ""))
	}

	y.SetPolicy(storePolicyFunc(func(ctx context.Context, tx *bolt.Tx, actor string, act Action, obj Object) error {
		if act == ActSee && obj.Kind == KindBead && obj.Name == hidden && actor == "mckean" {
			return fmt.Errorf("hidden from %s", actor)
		}
		return CorePolicy.May(ctx, tx, actor, act, obj)
	}))
	check("with a bead hidden from mckean")
	for _, row := range userQueue(t, y, "mckean", "mckean") {
		if len(row) >= len(hidden) && row[:len(hidden)] == hidden {
			t.Fatalf("viewer mckean sees the hidden bead's ask: %v", row)
		}
	}

	y.tick(25 * time.Hour)
	check("after the put aside came back")
}

// A store from before the index has none of its asks in it, and a queue
// reads none; the migration from 36 puts them all back, and a store already
// at 37 is left as it is.
func TestMigrateAskIndexFillsAnOldStore(t *testing.T) {
	y, _ := queueYard(t)
	want := userQueue(t, y, "mckean", "")
	if len(want) == 0 {
		t.Fatal("the yard's queue is empty before the migration")
	}

	rebuild := func(from uint64) {
		t.Helper()
		if err := y.Store.db.Update(func(tx *bolt.Tx) error {
			if err := tx.DeleteBucket(bAskTo); err != nil {
				return err
			}
			if _, err := tx.CreateBucket(bAskTo); err != nil {
				return err
			}
			return migrateAskIndex(tx, from)
		}); err != nil {
			t.Fatal(err)
		}
	}
	rebuild(37)
	if got := userQueue(t, y, "mckean", ""); len(got) != 0 {
		t.Fatalf("a store at 37 with an empty index: queue = %v, want none; the migration ran again", got)
	}
	rebuild(36)
	if got := userQueue(t, y, "mckean", ""); !slices.Equal(got, want) {
		t.Fatalf("after the migration from 36: queue = %v, want %v", got, want)
	}
}
