package dispatch

import (
	"context"
	"testing"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// The session's idle nudge names the command its last move was refused for,
// until the bead moves on: an agent that took its refused done for done is
// told what to run. An ack that re-runs the done moves the bead, and the
// nudge says nothing after that.
func TestRefusedMoveNamesTheRefusedDone(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	ctx := context.Background()
	if err := st.PutDepot(ctx, store.Depot{Name: "yard", Base: "main"}, "me"); err != nil {
		t.Fatal(err)
	}
	f, err := store.ParseFlow("new -> review on done\nreview -> approved on done\napproved -> merged\nmerged terminal\n")
	if err != nil {
		t.Fatal(err)
	}
	f.Name = "yard"
	if _, err := st.PutFlow(ctx, *f, "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFlowPointer(ctx, "yard", "", "yard", "me"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutGroup(ctx, store.Group{Name: "g", Runner: "fake", Limit: 2}, "me"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateBead(ctx, store.NewBead{ID: "yd-1", Title: "yd-1", Depot: "yard", Stage: "new"}, "me"); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Claim(ctx, "yd-1", "g", "new", "yard")
	if err != nil {
		t.Fatal(err)
	}
	note, err := st.AddNotedAs(ctx, "yd-1", "narrower: only the shed", "yardmaster", "")
	if err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{Store: st}
	if command, _, ok := d.refusedMove(ctx, sess); ok {
		t.Fatalf("a session with no refused move names %q", command)
	}

	if _, err := st.Done(ctx, "yd-1", "", "built it", store.Expect{Session: sess.ID}, "builder"); err == nil {
		t.Fatal("done with an unread note passed")
	}
	command, reason, ok := d.refusedMove(ctx, sess)
	if !ok || command != "yardr bead done yd-1" || reason != store.RefusedUnread {
		t.Fatalf("refused move = %q, %q, %v; want the done, refused as unread", command, reason, ok)
	}

	if _, err := st.AckNote(ctx, "yd-1", note.Seq, sess.ID, "builder"); err != nil {
		t.Fatal(err)
	}
	if re, err := st.RetryRefusedDone(ctx, "yd-1", sess.ID, "builder"); err != nil || re == nil {
		t.Fatalf("retry after the ack = %+v, %v; want the done re-run", re, err)
	}
	if command, _, ok := d.refusedMove(ctx, sess); ok {
		t.Fatalf("a bead that moved on still names %q", command)
	}
}
