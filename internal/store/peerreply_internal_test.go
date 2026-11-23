package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// receiptIndexed is the seq the wire id index holds for id, and whether it
// holds one.
func receiptIndexed(t *testing.T, s *Store, id string) (uint64, bool) {
	t.Helper()
	var seq []byte
	if err := s.read(func(tx *bolt.Tx) error {
		seq = append([]byte(nil), tx.Bucket(bMailReceiptIDs).Get([]byte(id))...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seq == nil {
		return 0, false
	}
	return binary.BigEndian.Uint64(seq), true
}

// mailReplyMsg is a reply from ada@b to jo, as the peer's yardr sends a mail
// that answers re.
func mailReplyMsg(t *testing.T, id, re string) PeerMessage {
	t.Helper()
	body, err := json.Marshal(peerMail{To: "mckean", From: "mckean", Text: "answer " + id})
	if err != nil {
		t.Fatal(err)
	}
	return PeerMessage{ID: id, Kind: PeerMail, Re: re, Body: body}
}

// TestReceiptsFollowTheirEventThroughPrune holds the mapping to what the
// log keeps: a receipt goes with its event when the event is pruned, but not
// while the outbox still holds the message it was queued under, which is
// custody. Acknowledging that message does not expire the receipt; the next
// prune that finds the event gone does.
func TestReceiptsFollowTheirEventThroughPrune(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)

	held, err := s.MailPeerReceipt(ctx, "mckean", "b", "held in the outbox", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	acked, err := s.MailPeerReceipt(ctx, "mckean", "b", "acknowledged", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PeerOutbox(ctx, "b", 0)
	if err != nil || len(out) != 2 {
		t.Fatalf("outbox = %+v, %v", out, err)
	}
	for _, m := range out {
		if m.ID == acked.ID {
			if err := s.PeerDelivered(ctx, "b", m.Seq); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A later event keeps the log's head past both, so they are old and
	// before it: the prune may take them.
	if err := s.Mail(ctx, "mckean", "after both", "mckean"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(ctx, Retention{Events: time.Nanosecond}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptIndexed(t, s, acked.ID); ok {
		t.Fatalf("acknowledged mail's receipt outlived its event")
	}
	if _, err := s.MailReceiptOf(ctx, acked.Seq, "mckean"); err == nil {
		t.Fatalf("receipt of a pruned event is still read")
	}
	if seq, ok := receiptIndexed(t, s, held.ID); !ok || seq != held.Seq {
		t.Fatalf("custody lost its receipt: index %d, %v", seq, ok)
	}

	// The outbox lets go of the held message, and the next prune takes the
	// receipt with the event it named.
	out, err = s.PeerOutbox(ctx, "b", 0)
	if err != nil || len(out) != 1 {
		t.Fatalf("outbox = %+v, %v", out, err)
	}
	if err := s.PeerDelivered(ctx, "b", out[0].Seq); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prune(ctx, Retention{Events: time.Nanosecond}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, ok := receiptIndexed(t, s, held.ID); ok {
		t.Fatalf("receipt of a pruned event outlived its custody")
	}
	if n := receiptKeys(t, s); n != 0 {
		t.Fatalf("receipts = %d, want none once their events are pruned", n)
	}
}

// TestReplyCorrelatesAfterRestart holds the mapping to a restart: the origin
// is closed and opened again, with its outbox message delivered, and a reply
// that names the mail still relates to the original event.
func TestReplyCorrelatesAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "a.db")
	s := receiptYard(t, path, true)
	r, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	s = receiptYard(t, path, false)
	defer s.Close(ctx)
	if _, err := s.AllowPeer(ctx, "b", PeerAllow{Receive: []string{PeerMail}, Expose: []string{"mckean"}}, "jo"); err != nil {
		t.Fatal(err)
	}
	got, err := s.PeerReceive(ctx, "b", r.PeerKey, []PeerMessage{mailReplyMsg(t, "after-restart", r.ID)})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("PeerReceive = %+v, %v", got, err)
	}
	var e Event
	evs, err := s.Events(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range evs {
		if x.Kind == "mail" && x.Data["id"] == "after-restart" {
			e = x
		}
	}
	if v, ok := e.Data["in_reply_to"].(float64); !ok || uint64(v) != r.Seq {
		t.Fatalf("in_reply_to = %v, want the original %d", e.Data["in_reply_to"], r.Seq)
	}
}

// TestWireIndexBuiltForAStoreWithReceipts holds the upgrade: a store whose
// receipts were written before the wire id index existed gets it filled, so
// a reply to any of them relates again.
func TestWireIndexBuiltForAStoreWithReceipts(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	r, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bMailReceiptIDs); err != nil {
			return err
		}
		return indexMailReceipts(tx)
	}); err != nil {
		t.Fatal(err)
	}
	if seq, ok := receiptIndexed(t, s, r.ID); !ok || seq != r.Seq {
		t.Fatalf("rebuilt index = %d, %v; want %d", seq, ok, r.Seq)
	}
}

// TestLegacyOutboxMessageHasNoRelation is a mail queued before receipts
// existed: its message is in the outbox but nothing maps its wire id, so a
// reply to it is taken as ordinary mail.
func TestLegacyOutboxMessageHasNoRelation(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	r, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket(bMailReceiptIDs).Delete([]byte(r.ID)); err != nil {
			return err
		}
		return tx.Bucket(bMailReceipts).Delete([]byte(seqKey(r.Seq)))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AllowPeer(ctx, "b", PeerAllow{Receive: []string{PeerMail}, Expose: []string{"mckean"}}, "jo"); err != nil {
		t.Fatal(err)
	}
	got, err := s.PeerReceive(ctx, "b", r.PeerKey, []PeerMessage{mailReplyMsg(t, "legacy", r.ID)})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("PeerReceive = %+v, %v", got, err)
	}
	evs, err := s.Events(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == "mail" && e.Data["id"] == "legacy" {
			if _, ok := e.Data["in_reply_to"]; ok {
				t.Fatalf("legacy reply relates: %v", e.Data["in_reply_to"])
			}
			if e.Data["re"] != r.ID {
				t.Fatalf("re = %v, want %s", e.Data["re"], r.ID)
			}
			return
		}
	}
	t.Fatal("legacy reply was not delivered")
}

// TestReceiptIndexRollsBackWithTheMail fails after the receipt and its wire
// id entry are written, the last of the mail's writes: the entry must go
// with the rest of the transaction.
func TestReceiptIndexRollsBackWithTheMail(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)

	orig := writeReceipt
	writeReceipt = func(tx *bolt.Tx, r MailReceipt) error {
		if err := orig(tx, r); err != nil {
			return err
		}
		return errors.New("write refused after the index")
	}
	defer func() { writeReceipt = orig }()

	if _, err := s.MailPeerReceipt(ctx, "mckean", "b", "peer", "mckean"); err == nil {
		t.Fatal("peer mail was queued with its write refused")
	}
	var ids int
	if err := s.read(func(tx *bolt.Tx) error {
		ids = len(keys(tx, bMailReceiptIDs, ""))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ids != 0 || receiptKeys(t, s) != 0 {
		t.Fatalf("index entries = %d, receipts = %d after the refused write, want none", ids, receiptKeys(t, s))
	}
}
