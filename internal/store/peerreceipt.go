package store

import (
	"context"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bMailReceipts holds a MailReceipt per mail this yard wrote, local or
// queued for a peer, keyed by the seq of the mail's event in this yard's
// log. It is kept apart from the outbox on purpose: the outbox message
// goes when the peer has it (PeerDelivered), and a receipt must outlive
// that, as the event does. Nothing here prunes it; it is as long-lived as
// the event it names.
var bMailReceipts = []byte("mail_receipts") // <event seq>: what a mail was

func init() { allBuckets = append(allBuckets, bMailReceipts) }

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
	return put(tx, bMailReceipts, seqKey(r.Seq), r)
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
