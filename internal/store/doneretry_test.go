package store_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// refusedDoneYard is a bead in new with a builder claimed on it, and the
// builder's done refused for a note the yardmaster wrote, which is unread.
// It returns the note's seq and the seq of the refusal the done left.
func refusedDoneYard(t *testing.T, notes int) (store.API, context.Context, *store.Bead, *store.Session, []uint64) {
	t.Helper()
	s, ctx := flowDepot(t)
	putFlow(t, s, "yard", "", outcomeFlow)
	if err := s.PutGroup(ctx, store.Group{Name: "builders", Runner: "herdr"}, "me"); err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateBead(ctx, store.NewBead{Title: "x", Depot: "yard", Stage: "new"}, "me")
	if err != nil {
		t.Fatal(err)
	}
	builder, err := s.Claim(ctx, b.ID, "builders", "new", "yard")
	if err != nil {
		t.Fatal(err)
	}
	var seqs []uint64
	for i := 0; i < notes; i++ {
		n, err := s.AddNotedAs(ctx, b.ID, fmt.Sprintf("narrower now %d", i), "yardmaster", "")
		if err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, n.Seq)
	}
	return s, ctx, b, builder, seqs
}

// refusalsOf is the bead's move_refused events, oldest first.
func refusalsOf(t *testing.T, s store.API, ctx context.Context, bead string) []store.Event {
	t.Helper()
	evs, err := s.Events(ctx, bead, 200)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, e := range evs {
		if e.Kind == store.MoveRefused {
			out = append(out, e)
		}
	}
	return out
}

// A done refused for an unread note moves the bead once that note is acked,
// with no further command: the ack re-runs the done under the same session,
// and the advanced event names the refusal it retried.
func TestAckRerunsRefusedDone(t *testing.T) {
	t.Parallel()
	s, ctx, b, builder, seqs := refusedDoneYard(t, 1)
	mine := store.Expect{Session: builder.ID}

	_, err := s.Done(ctx, b.ID, "", "built it", mine, "builder")
	if err == nil || !strings.Contains(err.Error(), "unread") {
		t.Fatalf("done with an unread note: %v, want it refused for the unread note", err)
	}
	refused := refusalsOf(t, s, ctx, b.ID)
	if len(refused) != 1 || refused[0].Data["reason"] != store.RefusedUnread {
		t.Fatalf("refusals = %+v, want one unread refusal", refused)
	}

	// Nothing acked: nothing to re-run.
	if re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder"); re != nil || err != nil {
		t.Fatalf("retry before the ack = %+v, %v; want nothing", re, err)
	}
	if cur, _ := s.GetBead(ctx, b.ID); cur.Stage != "new" {
		t.Fatalf("bead moved to %s before the ack", cur.Stage)
	}

	if _, err := s.AckNote(ctx, b.ID, seqs[0], builder.ID, "builder"); err != nil {
		t.Fatal(err)
	}
	re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder")
	if err != nil || re == nil {
		t.Fatalf("retry after the ack = %+v, %v; want the done re-run", re, err)
	}
	if want := store.DoneCommand(b.ID, store.DefaultOutcome); re.Command != want {
		t.Errorf("re-ran %q, want %q", re.Command, want)
	}
	if re.Bead.Stage != "review" {
		t.Fatalf("re-run done left the bead in %s, want review", re.Bead.Stage)
	}

	evs, err := s.Events(ctx, b.ID, 200)
	if err != nil {
		t.Fatal(err)
	}
	var advanced *store.Event
	for i := range evs {
		if evs[i].Kind == "advanced" {
			advanced = &evs[i]
		}
	}
	if advanced == nil {
		t.Fatal("the re-run left no advanced event")
	}
	if got, want := fmt.Sprint(advanced.Data["retried"]), fmt.Sprint(refused[0].Seq); got != want {
		t.Errorf("advanced event retried %q, want the refusal's seq %q", got, want)
	}

	// The bead has moved: no second re-run.
	if re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder"); re != nil || err != nil {
		t.Fatalf("second retry = %+v, %v; want nothing", re, err)
	}
}

// The re-run is a done like any other: a failing gate still refuses it.
// Here the worktree has an untracked file, so the ack leaves the bead where
// it is and the retry says why.
func TestAckRerunKeepsItsGates(t *testing.T) {
	t.Parallel()
	s, ctx, b, builder, seqs := refusedDoneYard(t, 1)
	dir := gitRepo(t, map[string]string{"main.go": "package main\n"})
	if err := s.SetHandle(ctx, builder.ID, map[string]any{"worktree": dir}); err != nil {
		t.Fatal(err)
	}
	mine := store.Expect{Session: builder.ID}

	if _, err := s.Done(ctx, b.ID, "", "built it", mine, "builder"); err == nil || !strings.Contains(err.Error(), "unread") {
		t.Fatalf("done with an unread note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.py"), []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AckNote(ctx, b.ID, seqs[0], builder.ID, "builder"); err != nil {
		t.Fatal(err)
	}

	re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder")
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") || re != nil {
		t.Fatalf("retry with a dirty worktree = %+v, %v; want the uncommitted changes refusal", re, err)
	}
	if cur, _ := s.GetBead(ctx, b.ID); cur.Stage != "new" {
		t.Fatalf("refused re-run moved the bead to %s", cur.Stage)
	}
	if refused := refusalsOf(t, s, ctx, b.ID); len(refused) < 2 {
		t.Fatalf("refusals = %+v, want the re-run's refusal logged too", refused)
	}
}

// Two unread notes: acking one leaves the other unread, so nothing is
// re-run; acking the last one re-runs the done.
func TestAckRerunWaitsForEveryAck(t *testing.T) {
	t.Parallel()
	s, ctx, b, builder, seqs := refusedDoneYard(t, 2)
	mine := store.Expect{Session: builder.ID}

	if _, err := s.Done(ctx, b.ID, "", "built it", mine, "builder"); err == nil || !strings.Contains(err.Error(), "unread") {
		t.Fatalf("done with two unread notes: %v", err)
	}
	if _, err := s.AckNote(ctx, b.ID, seqs[0], builder.ID, "builder"); err != nil {
		t.Fatal(err)
	}
	if re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder"); re != nil || err != nil {
		t.Fatalf("retry with one note still unread = %+v, %v; want nothing", re, err)
	}
	if cur, _ := s.GetBead(ctx, b.ID); cur.Stage != "new" {
		t.Fatalf("bead moved to %s with a note unread", cur.Stage)
	}

	if _, err := s.AckNote(ctx, b.ID, seqs[1], builder.ID, "builder"); err != nil {
		t.Fatal(err)
	}
	re, err := s.RetryRefusedDone(ctx, b.ID, builder.ID, "builder")
	if err != nil || re == nil || re.Bead.Stage != "review" {
		t.Fatalf("retry with every note acked = %+v, %v; want the bead in review", re, err)
	}
}
