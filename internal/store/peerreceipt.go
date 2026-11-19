package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bMailReceipts holds a MailReceipt per mail this yard wrote, local or
// queued for a peer, keyed by the seq of the mail's event in this yard's
// log. It is kept apart from the outbox on purpose: the outbox message
// goes when the peer has it (PeerDelivered), and a receipt must outlive
// that, as the event does: pruneMailReceipts drops it with the event it
// names, once the outbox no longer holds the message it was queued under.
var bMailReceipts = []byte("mail_receipts") // <event seq>: what a mail was

// bMailReceiptIDs finds a receipt by the wire id of the mail it names, so
// the reply that names that id (Re) is checked against its mail without a
// scan. Only an outbound receipt with an id is in it.
var bMailReceiptIDs = []byte("mail_receipt_ids") // <wire id>: <event seq>

func init() { allBuckets = append(allBuckets, bMailReceipts, bMailReceiptIDs) }

// ReceiptLocal and ReceiptOut are the directions of a receipt: mail this
// yard delivered to its own inboxes, and mail it queued for a peer.
const (
	ReceiptLocal = "local"
	ReceiptOut   = "out"
)

// MailReceipt is this yard's record of one mail it wrote: the seq of its
// event in this yard's log, the wire id the peer is sent the message
// under (a peer mail only), the direction, the peer as the link named it
// and the public key the link authenticated for it (a peer mail only),
// and the participants, From this yard and To the crew member or user it
// names.
//
// A receipt proves only what this yard committed: the mail was written to
// the log, and for a peer mail queued in the outbox for the peer, in one
// write. It is not evidence that the peer took the message. Peer is the
// name the link had when the mail was queued; a later rename does not
// change it, and PeerKey is what identifies the peer.
type MailReceipt struct {
	Seq       uint64    `json:"seq"`
	ID        string    `json:"id,omitempty"`
	Direction string    `json:"direction"`
	Peer      string    `json:"peer,omitempty"`
	PeerKey   string    `json:"peer_key,omitempty"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        time.Time `json:"at"`
}

// writeReceipt puts r in the transaction its mail is written in, so the
// receipt exists exactly when the mail does. It is a variable only so the
// rollback test can fail the last write of a mail, after its event and
// outbox message are in the same transaction.
var writeReceipt = func(tx *bolt.Tx, r MailReceipt) error {
	if err := put(tx, bMailReceipts, seqKey(r.Seq), r); err != nil {
		return err
	}
	if r.Direction != ReceiptOut || r.ID == "" {
		return nil
	}
	return tx.Bucket(bMailReceiptIDs).Put([]byte(r.ID), []byte(seqKey(r.Seq)))
}

// answeredMail is the outbound mail a reply from p names by re, when it is
// the mail this yard queued for p and the reply runs back along it: the
// receipt's peer is p by key, not by name, so a link renamed keeps it and
// one removed and made again under another key does not; the reply's
// sender is the mail's recipient and its recipient the mail's sender. ok
// is false for any other: an unknown or pruned id, another peer's key, or
// participants that do not match. Nothing is taken from the reply's text
// or its place in the log.
func (w *wtx) answeredMail(p *Peer, re string, mail *peerMail) (MailReceipt, bool, error) {
	var r MailReceipt
	if re == "" {
		return r, false, nil
	}
	seq := w.Bucket(bMailReceiptIDs).Get([]byte(re))
	if seq == nil {
		return r, false, nil
	}
	ok, err := get(w.Tx, bMailReceipts, string(seq), &r)
	if err != nil || !ok {
		return MailReceipt{}, false, err
	}
	if r.Direction != ReceiptOut || r.ID != re || r.PeerKey != p.Key || r.To != mail.From || r.From != mail.To {
		return MailReceipt{}, false, nil
	}
	return r, true, nil
}

// indexMailReceipts builds the wire id index from the receipts a store
// written before it had one. It runs where the buckets of a store from
// before are not yet created, so a store with no receipts gets an empty
// index from createBuckets, and one with receipts gets it filled here, once.
func indexMailReceipts(tx *bolt.Tx) error {
	if tx.Bucket(bMailReceiptIDs) != nil || tx.Bucket(bMailReceipts) == nil {
		return nil
	}
	if _, err := tx.CreateBucket(bMailReceiptIDs); err != nil {
		return err
	}
	return each(tx, bMailReceipts, "", func(_ []byte, r *MailReceipt) error {
		if r.Direction != ReceiptOut || r.ID == "" {
			return nil
		}
		return tx.Bucket(bMailReceiptIDs).Put([]byte(r.ID), []byte(seqKey(r.Seq)))
	})
}

// pruneMailReceipts drops the receipts of mail whose event the log no
// longer holds. The log is pruned from its oldest end, so every receipt
// older than its first remaining event names an event that is gone. A
// receipt whose wire id the outbox still holds stays: the queued message is
// custody that names it, and the next prune finds it again once the
// message has gone. Nothing else removes a receipt, so an acknowledged
// message does not expire one whose event is kept.
func pruneMailReceipts(tx *bolt.Tx) error {
	first := uint64(math.MaxUint64) // no event left: every receipt is gone
	if k, _ := tx.Bucket(bEvents).Cursor().First(); k != nil {
		first = binary.BigEndian.Uint64(k)
	}
	type gone struct {
		key []byte
		id  string
	}
	var old []gone
	c := tx.Bucket(bMailReceipts).Cursor()
	for k, raw := c.First(); k != nil && binary.BigEndian.Uint64(k) < first; k, raw = c.Next() {
		var r MailReceipt
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		old = append(old, gone{append([]byte(nil), k...), r.ID})
	}
	if len(old) == 0 {
		return nil
	}
	queued := map[string]bool{}
	if err := each(tx, bPeerOut, "", func(_ []byte, m *PeerOut) error {
		queued[m.ID] = true
		return nil
	}); err != nil {
		return err
	}
	for _, g := range old {
		if g.id != "" && queued[g.id] {
			continue
		}
		if g.id != "" {
			if err := tx.Bucket(bMailReceiptIDs).Delete([]byte(g.id)); err != nil {
				return err
			}
		}
		if err := tx.Bucket(bMailReceipts).Delete(g.key); err != nil {
			return err
		}
	}
	return nil
}

// MailReceiptOf is the receipt of the mail whose event is seq, for actor:
// the mail it sent. Another name's mail, and a seq that is no receipt, are
// ErrNotFound alike, so the answer tells nothing of other mail. A pane's
// call is read as the session's or crew member's name, as in the writes.
func (s *Store) MailReceiptOf(ctx context.Context, seq uint64, actor string) (MailReceipt, error) {
	var r MailReceipt
	err := s.read(func(tx *bolt.Tx) error {
		resolved, err := paneAuthor(tx, callerOf(ctx), actor)
		if err != nil {
			return err
		}
		ok, err := get(tx, bMailReceipts, seqKey(seq), &r)
		if err != nil {
			return err
		}
		if !ok || r.From != resolved {
			return fmt.Errorf("mail %d: %w", seq, ErrNotFound)
		}
		return nil
	})
	return r, err
}

// MailPeerReceipts sends text to the crew member or user to of peer, as
// MailPeerReceipt sends one mail, in as many mails as SplitMail makes of
// it (as MailText does for local mail), and returns the receipt of each
// part sent, in order. Files go with the first part only. A part that is
// not taken ends the send: the receipts of the parts before it are
// returned with the error, and no later part is sent without it, as with
// mailParts.
func MailPeerReceipts(api API, ctx context.Context, to, peer, text, actor string, files ...BlobFile) ([]MailReceipt, error) {
	var got []MailReceipt
	err := mailParts(to, text, func(i int, part string) error {
		var first []BlobFile
		if i == 0 {
			first = files
		}
		r, err := api.MailPeerReceipt(ctx, to, peer, part, actor, first...)
		if err != nil {
			return err
		}
		got = append(got, r)
		return nil
	})
	return got, err
}
