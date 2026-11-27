package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"yardr.dev/yardr/internal/store"
)

// The enrollment of a legacy link (docs/peers.md, "Enrolling a legacy link"):
// the --enroll flags read into what a pairing is opened with, the outcomes a
// pairing with an enrollment can end in, and what each side says of them.

// enrollRename says, before the check is compared, that the peer being
// enrolled calls itself by another name than this yard has saved. The check
// covers the name too, so both operators see it; this line is what makes
// the change visible to the one who confirms it.
func (a *app) enrollRename(w, warn io.Writer, peer, saved, calls string) {
	if saved == "" || saved == calls {
		return
	}
	a.progress(w, warn, "%s calls itself %s now; this yard has it saved as %s. Confirm the check only if the other operator knows it was renamed.", peer, calls, saved)
}

// peerEnrollment reads the --enroll flags into what the pairing is opened
// with: the existing peer as it is now, and the fresh invite the operator
// obtained for it. Nothing is changed here; the store refuses a peer that
// has moved on since.
func peerEnrollment(ctx context.Context, st store.API, opt pairOpts) (*store.PeerEnrollment, error) {
	if opt.enroll == "" {
		return nil, nil
	}
	invite, err := store.ParseInvite(opt.invite)
	if err != nil {
		return nil, err
	}
	if invite.Enc == "" {
		return nil, errors.New("peer enrollment requires a yardr-peer-2 invite with an encryption key")
	}
	expected, err := st.GetPeer(ctx, opt.enroll)
	if err != nil {
		return nil, fmt.Errorf("peer %s: %w", opt.enroll, err)
	}
	// The name the peer calls itself may have changed since it was linked:
	// it is shown in the check, and the key, encryption key and URL must hold.
	if expected.Key != invite.Key || expected.URL != invite.URL ||
		(expected.Enc != "" && expected.Enc != invite.Enc) {
		return nil, fmt.Errorf("peer %s identity or authority does not match the fresh invite", opt.enroll)
	}
	return &store.PeerEnrollment{PeerName: opt.enroll, Expected: *expected, Invite: invite}, nil
}

// peerEnrollmentPartial says an enrollment ended with an outcome this yard
// cannot see, and how to find out and retry without losing the peer.
func peerEnrollmentPartial(peer, reason string) error {
	return fmt.Errorf("peer %s enrollment has a partial or unknown outcome: %s. Inspect `yardr peer show %s` on both yards. If both current peer records and independently obtained fresh invites still agree, open a new pairing on the reachable yard with `yardr peer pair --enroll %s --invite <fresh-v2-invite>`, then call it with `yardr peer pair <address> <new-code> --enroll %s --invite <fresh-v2-invite>`; both operators compare fresh checks. Do not remove, re-add, clear a pin, or replay queued work", peer, reason, peer, peer, peer)
}

// pairCallerCommand is the command the other yard's operator runs. An
// enrollment's caller needs its own existing peer name and an invite it
// obtained independently, neither of which this yard knows.
func pairCallerCommand(address, code string, enroll bool) string {
	command := fmt.Sprintf("yardr peer pair %s %s", address, code)
	if enroll {
		command += " --enroll <existing-peer-name> --invite '<fresh-v2-invite>'"
	}
	return command
}

// pairEnrollmentDone says what an enrollment did, and that the link's
// settings and queued work were kept.
func (a *app) pairEnrollmentDone(w io.Writer, p store.Peer, already bool) error {
	label := "enrolled"
	if already {
		label = "already enrolled"
	}
	return a.out(w, map[string]any{"peer": p, "status": label}, func(w io.Writer) {
		fmt.Fprintf(w, "peer %s: %s; its link settings and queued work were preserved\n", p.Name, label)
		fmt.Fprintf(w, "Inspect both sides with yardr peer show %s; no permissions or ping were added\n", p.Name)
	})
}
