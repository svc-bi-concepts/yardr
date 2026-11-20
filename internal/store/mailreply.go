package store

import (
	"context"
	"fmt"
	"maps"
	"strings"
)

// MailReply answers the mail answered: one mail to its sender, carrying the
// answered seq on its event (answeredSeq), as yardr reply sends it. The
// answered mail must be mail to the actor; mail from another yard, mail
// from a bead's session and mail from a yard no longer linked are refused
// as yardr reply refuses them, in the words of the window that answers.
// Longer answers are split by the caller (MailReplyText), as with Mail.
func (s *Store) MailReply(ctx context.Context, to, text, actor string, answered uint64) error {
	_, err := s.mailReply(ctx, to, text, actor, answered)
	return err
}

// MailReplyReceipt is MailReply, and returns the receipt the answer was
// written under (MailReceipt).
func (s *Store) MailReplyReceipt(ctx context.Context, to, text, actor string, answered uint64) (MailReceipt, error) {
	return s.mailReply(ctx, to, text, actor, answered)
}

func (s *Store) mailReply(ctx context.Context, to, text, actor string, answered uint64) (MailReceipt, error) {
	if strings.TrimSpace(text) == "" || len(text) > MaxMailBytes {
		return MailReceipt{}, fmt.Errorf("mail must be 1..%d bytes", MaxMailBytes)
	}
	var got MailReceipt
	err := s.writeVia(ctx, func(w *wtx) error {
		resolved, sess, err := paneAuthorSession(w.Tx, callerOf(ctx), actor)
		if err != nil {
			return err
		}
		actor = resolved
		var e Event
		if ok, err := get(w.Tx, bEvents, seqKey(answered), &e); err != nil {
			return err
		} else if !ok {
			return fmt.Errorf("message %d: %w", answered, ErrNotFound)
		}
		if e.Kind != "mail" {
			return fmt.Errorf("message %d is a %s event, not mail: only mail is answered", answered, e.Kind)
		}
		if MailReceiver(e) != actor {
			return fmt.Errorf("message %d is not mail to %s", answered, actor)
		}
		// Under the names its yard has now: a rename since moves the
		// sender, and a link gone since reads as unlinked, as in an
		// inbox, so the refusals below answer today's link.
		evs := []Event{e}
		if err := peersToday(w.Tx, evs); err != nil {
			return err
		}
		e = evs[0]
		if gone, _ := e.Data["from_peer_unlinked"].(bool); gone {
			from, _ := e.Data["from"].(string)
			was, _ := e.Data["from_peer_was"].(string)
			return fmt.Errorf("message %d is mail from %s at a yard no longer linked (was %s): no address would reach its sender", answered, from, was)
		}
		if peer, _ := e.Data["from_peer"].(string); peer != "" {
			return fmt.Errorf("message %d is from another yard (%s), which this window does not answer. Answer it in a terminal on the machine the yard runs on: `yardr mail %s <your answer>` (a crew member's mail there is answered with `yardr reply`)", answered, e.Actor, e.Actor)
		}
		if bead, _ := e.Data["from_bead"].(string); bead != "" {
			return fmt.Errorf("message %d came from a session doing your work, and its answer is a note on that work, which this window does not write. Answer it from footplate, or ask the yardmaster", answered)
		}
		if to != e.Actor {
			return fmt.Errorf("message %d is from %s, not %s: a reply goes to the mail's sender", answered, e.Actor, to)
		}
		r, err := s.sendMailReceipt(w, to, text, actor, sess, nil, answered, nil)
		got = r
		return err
	})
	if err != nil {
		return MailReceipt{}, err
	}
	return got, nil
}

// sendMail writes one mail to the inbox of a crew member or of a user, as
// Mail sends it, carrying answered where it answers mail, 0 for a new
// message. actor is resolved (paneAuthorSession) and sess its session, if
// the call comes from a bead session's pane, whose bead the mail then
// names. fields are the data the event carries beyond its text, as MailFromPeer
// gives a mail its sender. The recipient checks read as Mail's.
func (s *Store) sendMail(w *wtx, to, text, actor string, sess *Session, refs []BlobRef, answered uint64, fields map[string]any) error {
	_, err := s.sendMailReceipt(w, to, text, actor, sess, refs, answered, fields)
	return err
}

// sendMailReceipt returns the receipt of the mail, written in the same
// transaction as its event.
func (s *Store) sendMailReceipt(w *wtx, to, text, actor string, sess *Session, refs []BlobRef, answered uint64, fields map[string]any) (MailReceipt, error) {
	data := map[string]any{"to": to, "text": text}
	maps.Copy(data, fields)
	if len(refs) > 0 {
		data["files"] = refs
	}
	if answered != 0 {
		data["answered"] = answered
	}
	if sess != nil {
		// In the data and not on the event's bead: the event would
		// then reach the bead's subscribers and its history, which
		// are not the mail's readers.
		data["from_bead"] = handlesIn(w.Tx).of(sess.Bead)
	}
	switch {
	case w.Bucket(bCrew).Get([]byte(to)) != nil:
		var r CrewMember
		if ok, err := get(w.Tx, bCrew, to, &r); err != nil {
			return MailReceipt{}, err
		} else if ok && w.Bucket(bUsers).Get([]byte(actor)) != nil && !r.Serves(actor) {
			// Mail from a user it does not serve is refused at write,
			// naming whom it serves; anything else (the yard, another
			// crew member, a bead's agent) still lands, for the line
			// slice, or waits in the log where no line session runs.
			return MailReceipt{}, fmt.Errorf("mail to %s from %s: %s serves %s", to, actor, to, strings.Join(r.Served(), ", "))
		}
	case w.Bucket(bUsers).Get([]byte(to)) != nil:
		data["to_user"] = true
	default:
		return MailReceipt{}, fmt.Errorf("crew member %s: %w (nor is there a user of that name)", to, ErrNotFound)
	}
	if err := w.event("", "mail", actor, data); err != nil {
		return MailReceipt{}, err
	}
	// The event is the last one written, so its seq is the sequence the
	// write just took: no scan for it. The receipt goes in this write, so
	// the mail and its receipt commit together or not at all.
	r := MailReceipt{Seq: w.Bucket(bEvents).Sequence(), Direction: ReceiptLocal, From: actor, To: to, At: now()}
	if err := writeReceipt(w.Tx, r); err != nil {
		return MailReceipt{}, err
	}
	return r, nil
}

// answeredSeq is the seq of the mail the mail event e answers, 0 for mail
// that answers none. A number of the log reads back as a float64; the
// write holds the seq it was given.
func answeredSeq(e Event) uint64 {
	switch n := e.Data["answered"].(type) {
	case float64:
		if n >= 0 && n == float64(uint64(n)) {
			return uint64(n)
		}
	case uint64:
		return n
	case int:
		if n >= 0 {
			return uint64(n)
		}
	case int64:
		if n >= 0 {
			return uint64(n)
		}
	}
	return 0
}
