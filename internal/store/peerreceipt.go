package store

import (
	"context"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// bMailReceipts holds a MailReceipt per mail this yard queued for a peer,
// keyed by the seq of the mail's event in this yard's log. It is kept
// apart from the outbox on purpose: the outbox message goes when the peer
// has it (PeerDelivered), and a receipt must outlive that, as the event
// does. Nothing here prunes it; it is as long-lived as the event it names.
var bMailReceipts = []byte("mail_receipts") // <event seq>: what a mail to a peer was

func init() { allBuckets = append(allBuckets, bMailReceipts) }

// ReceiptOut is the direction of a receipt for mail this yard sent to a
// peer. It is the only one written so far: mail received from a peer has
// no receipt yet.
const ReceiptOut = "out"

// MailReceipt is this yard's record of one mail queued for a peer: the
// seq of its event in this yard's log, the wire id the peer is sent the
// message under, the direction, the peer as the link named it and the
// public key the link authenticated for it, and the participants, From
// this yard and To the crew member or user at the peer.
//
// A receipt proves only what this yard committed: the mail was written
// to the log and queued in the outbox for the peer, in one write. It is
// not evidence that the peer took the message. Peer is the name the link
// had when the mail was queued; a later rename does not change it, and
// PeerKey is what identifies the peer.
type MailReceipt struct {
	Seq       uint64    `json:"seq"`
	ID        string    `json:"id"`
	Direction string    `json:"direction"`
	Peer      string    `json:"peer"`
	PeerKey   string    `json:"peer_key"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	At        time.Time `json:"at"`
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
// part sent, in order. A part that is not taken ends the send: the
// receipts of the parts before it are returned with the error, and no
// later part is sent without it, as with mailParts.
func MailPeerReceipts(api API, ctx context.Context, to, peer, text, actor string) ([]MailReceipt, error) {
	var got []MailReceipt
	err := mailParts(to, text, func(_ int, part string) error {
		r, err := api.MailPeerReceipt(ctx, to, peer, part, actor)
		if err != nil {
			return err
		}
		got = append(got, r)
		return nil
	})
	return got, err
}
