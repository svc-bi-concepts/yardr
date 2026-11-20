package store

import (
	"context"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// Done reports how the work in the bead's stage ended and moves it where
// its flow sends that outcome ("" is done), with an optional note, in one
// write like AdvanceNote. The outcome is recorded on the advanced event.
// A bead no flow governs has no outcomes: it is moved with Advance.
// The session's own done is refused while a note on the bead names that
// session and is not acked (see checkUnread), and while its worktree has
// uncommitted changes (see checkWorktree). DoneSafe is refused while the
// bead's diff touches a path the depot lists (SecurityPathsFile). From a
// session's or a crew member's pane the move and its note are recorded as
// that session's actor or that crew member, whatever actor says; from
// anywhere else, as given (paneAuthor).
func (s *Store) Done(ctx context.Context, id, outcome, note string, exp Expect, actor string) (*Bead, error) {
	return s.done(ctx, id, outcome, note, exp, actor, 0)
}

// done is Done. retried is the seq of the move_refused a retry re-runs (see
// RetryRefusedDone), and 0 for any other done.
func (s *Store) done(ctx context.Context, id, outcome, note string, exp Expect, actor string, retried uint64) (*Bead, error) {
	outcome = or(strings.TrimSpace(outcome), DefaultOutcome)
	command := DoneCommand(id, outcome)
	// Read before the write: git must not run inside one.
	safe := s.checkDoneSafe(ctx, id, outcome)
	dirty := s.checkWorktree(ctx, id, exp)
	b, err := s.advance(ctx, id, note, exp, actor, true, donePick(outcome, safe, retried), dirty)
	// The done and its note are kept on an unread refusal, which the ack of
	// its notes re-runs (see RetryRefusedDone).
	return b, s.refusedWith(ctx, id, command, exp, actor, err, map[string]any{"outcome": outcome, "done_note": note})
}

// donePick reports outcome through the flow governing the bead, as Done
// does: the stage it sends a bead in its stage to, with the outcome on the
// event. An outcome whose limit the bead's rounds have used sends it to the
// limit's Then instead, and the event says so (rounds, max, instead_of); move
// starts the count again and logs rounds_reached. A report of DoneSafe that
// safe holds (checkDoneSafe) is refused once the flow has the outcome. An
// answer on a stage ask moves the bead through the same pick, with no safe: a
// person answers it. retried, when set, is the refusal this done re-ran.
func donePick(outcome string, safe *safeCheck, retried uint64) func(f *Flow, b *beadRow, data map[string]any) (string, error) {
	return func(f *Flow, b *beadRow, data map[string]any) (string, error) {
		if f == nil {
			return "", fmt.Errorf("bead %s: no flow governs it, so it has no outcomes; move it with yardr bead advance %s --to <stage>", b.ID, b.ID)
		}
		o, err := f.Outcome(b.Stage, outcome)
		if err != nil {
			return "", fmt.Errorf("bead %s: %w", b.ID, err)
		}
		if err := safe.refusal(f, b.ID, b.Stage); err != nil {
			return "", err
		}
		data["outcome"] = outcome
		if retried > 0 {
			data["retried"] = retried
		}
		n := b.takes(o)
		if n.Goes != o.To {
			data["rounds"], data["max"], data["instead_of"] = n.Used, o.Max, o.To
		}
		return n.Goes, nil
	}
}

// RetryRefusedDone re-runs the session's own done of bead id once the
// session has acked every note it was told of (yardr bead note --ack), when
// the bead's newest move_refused is that done refused for unread notes. The
// ack's command calls it, so the agent never has to run the done again: the
// re-run is the same session's done with the same outcome and note, and
// every check of a done runs again. It returns the command it re-ran and
// the bead it left, or "" and nil when it re-ran nothing: the refusal is
// not this session's unread one, a move has advanced the bead since it, or
// a note the session was told of is still unread. A re-run that is refused
// again returns that refusal, as the done itself would, and logs as any
// refusal does. The advanced event names the refusal it retried.
func (s *Store) RetryRefusedDone(ctx context.Context, id, session, actor string) (*RetriedMove, error) {
	r, err := s.unreadRefusal(ctx, id, session)
	if err != nil || r == nil {
		return nil, err
	}
	b, err := s.done(ctx, id, r.outcome, r.note, Expect{Session: r.session, Stage: r.stage}, actor, r.seq)
	if err != nil {
		return nil, err
	}
	return &RetriedMove{Command: DoneCommand(id, r.outcome), Bead: b}, nil
}

// RetriedMove is a done RetryRefusedDone re-ran: its command and the bead
// it left.
type RetriedMove struct {
	Command string
	Bead    *Bead
}

// unreadRefused is the refusal RetryRefusedDone re-runs: the done the
// session made, as move_refused kept it.
type unreadRefused struct {
	session, outcome, note, stage string
	seq                           uint64
}

// unreadRefusal reads the bead's refusal that a retry re-runs (see
// RetryRefusedDone), or nil. The session is the caller's pane's, as an ack
// names it (see AckNote).
func (s *Store) unreadRefusal(ctx context.Context, id, session string) (*unreadRefused, error) {
	var out *unreadRefused
	err := s.read(func(tx *bolt.Tx) error {
		pane, err := paneSession(tx, callerOf(ctx))
		if err != nil {
			return err
		}
		if pane != nil {
			session = pane.ID
		}
		if session == "" {
			return nil
		}
		uuid, err := resolve(tx, id)
		if err != nil {
			return err
		}
		// A note the session was told of and has not read still holds the
		// refusal: the retry waits for the last ack.
		unread := false
		if err := each(tx, bNotes, uuid+"\x00", func(_ []byte, n *Note) error {
			unread = unread || n.Notify == session && !n.Acked
			return nil
		}); err != nil || unread {
			return err
		}
		// Newest first: the first move of either kind decides. A move since
		// the refusal, or a refusal of any other kind, leaves nothing to re-run.
		ks := keys(tx, bEventsBy, uuid+"\x00")
		for i := len(ks) - 1; i >= 0; i-- {
			var e Event
			if _, err := get(tx, bEvents, ks[i][len(uuid)+1:], &e); err != nil {
				return err
			}
			if e.Kind == "advanced" {
				return nil
			}
			if e.Kind != MoveRefused {
				continue
			}
			if e.Data["session"] != session || e.Data["reason"] != RefusedUnread {
				return nil
			}
			outcome, _ := e.Data["outcome"].(string)
			note, _ := e.Data["done_note"].(string)
			stage, _ := e.Data["stage"].(string)
			out = &unreadRefused{session: session, outcome: outcome, note: note, stage: stage, seq: e.Seq}
			return nil
		}
		return nil
	})
	return out, err
}
