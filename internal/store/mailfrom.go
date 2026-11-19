package store

import (
	"context"
	"fmt"
	"strings"
)

// MailFromPeer writes mail to the inbox of a crew member or user of this
// yard, from from, a crew member or user of the linked yard peer. It is
// the record of mail that reached this yard without its link, written into
// its home by another yard's command (yardr mail --home): the event is as
// the link's own mail is written (deliverMail), from <from>@<peer> with
// from and from_peer, so a reply to it goes over the link, as to any mail
// from that yard. from must not be a crew member or user of this yard,
// whose mail is Mail's, and peer must be a link of this yard.
func (s *Store) MailFromPeer(ctx context.Context, to, text, from, peer string, files ...BlobFile) error {
	if strings.TrimSpace(text) == "" && len(files) == 0 || len(text) > MaxMailBytes {
		return fmt.Errorf("mail must be 1..%d bytes", MaxMailBytes)
	}
	refs, err := s.mailFiles(files)
	if err != nil {
		return fmt.Errorf("mail to %s: %w", to, err)
	}
	return s.writeVia(ctx, func(w *wtx) error {
		if kind, _, err := callerIn(w.Tx, ctx); err != nil {
			return err
		} else if kind == CallerCrew {
			return fmt.Errorf("mail from %s: a local crew member writes no mail of another yard", from)
		} else if kind == CallerBead || kind == CallerScript {
			return fmt.Errorf("mail from %s: a bead's session writes no mail of another yard", from)
		}
		if w.Bucket(bPeers).Get([]byte(peer)) == nil {
			return fmt.Errorf("peer %s: %w", peer, ErrNotFound)
		}
		if w.Bucket(bCrew).Get([]byte(from)) != nil || w.Bucket(bUsers).Get([]byte(from)) != nil {
			return fmt.Errorf("mail from %s: %s is a crew member or user of this yard, not of %s", from, from, peer)
		}
		return s.sendMail(w, to, text, from+"@"+peer, nil, refs, 0, map[string]any{"from": from, "from_peer": peer})
	})
}
