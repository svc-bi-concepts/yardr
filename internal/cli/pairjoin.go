package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/huh"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/yardkey"
	"yardr.dev/yardr/internal/yards"
)

// The calling side of a pairing: yardr peer pair <address> <code> asks the
// other yard for its answer, checks it, and saves the peer when both confirm.

// pairAccept asks whether the yard that answered a call can be a peer here,
// before this yard has revealed itself: the other operator is then asked
// nothing, and the code is still good for a call with --name. It answers
// the accept and where it puts what answered. A yard that cannot be one is
// logged here, and ends the call as errNotAdded's.
func (a *app) pairAccept(ctx context.Context, st store.API, inv store.Invite, addr, name string, form bool, yardr string, enrollment *store.PeerEnrollment) (func(host store.Party) error, *store.Peer) {
	var peer store.Peer
	who := a.who()
	accept := func(host store.Party) error {
		peer = store.Peer{Name: or(name, host.Name), Key: host.Key, Enc: host.Enc, URL: addr, Calls: host.Name}
		if host.Key == inv.Key {
			_ = st.PairRefused(ctx, store.Own, host.Name, who)
			return fmt.Errorf("%w: the yard that answered has this yard's own key, and a yard is not paired with itself", errNotAdded)
		}
		if enrollment != nil {
			// The peer is the one this yard already has: the answer must be
			// the yard its operator got the fresh invite from, at the address
			// it has, and nothing of it changes but the missing pin.
			existing := enrollment.Expected
			if host.Key != enrollment.Invite.Key || host.Name != enrollment.Invite.Name || host.Enc != enrollment.Invite.Enc ||
				host.URL != enrollment.Invite.URL || host.URL != existing.URL {
				return fmt.Errorf("peer %s does not match its independently obtained invite and unchanged URL", existing.Name)
			}
			peer = existing
			return nil
		}
		if err := st.CheckPeer(ctx, peer); err != nil {
			_ = st.PairRefused(ctx, store.AlreadyPeer, host.Name, who)
			if form {
				return fmt.Errorf("%w: %w. `%s peer list` shows this yard's peers; the code is still good for `%s peer pair <address> <code> --name <another name>`",
					errNotAdded, err, yardr, yardr)
			}
			if name == "" {
				return fmt.Errorf("%w: %w (--name saves the other yard under a name of your choosing; the code is still good)", errNotAdded, err)
			}
			return fmt.Errorf("%w: %w", errNotAdded, err)
		}
		return nil
	}
	return accept, &peer
}

// pairJoin calls the yard at address with the code its operator read out.
func (a *app) pairJoin(w, warn io.Writer, ctx context.Context, cfg *config.Config, st store.API, address, code string, opt pairOpts) error {
	addr, err := pairAddress(address)
	if err != nil {
		return err
	}
	clean, err := store.CleanCode(code)
	if err != nil {
		return err
	}
	inv, err := invite(ctx, cfg, st, "")
	if err != nil {
		return err
	}
	key, err := store.Load(cfg.PeerKey())
	if err != nil {
		return err
	}
	who := a.who()
	var existing *store.Peer
	var enrollment *store.PeerEnrollment
	if opt.enroll != "" {
		enrollment, err = peerEnrollment(ctx, st, opt)
		if err != nil {
			return err
		}
		existing = &enrollment.Expected
		if existing.URL == "" {
			return errors.New("the existing peer has no callable URL; open the pairing from that yard or restore its unchanged address first")
		}
		if existing.URL != addr {
			return fmt.Errorf("peer %s is called at %s, not %s", existing.Name, existing.URL, addr)
		}
	} else if opt.name != "" {
		// Said now, not after the other operator has been kept waiting. The
		// key is this yard's own, which is no peer's: only the name is asked.
		if err := st.CheckPeer(ctx, store.Peer{Name: opt.name, Key: inv.Key}); err != nil {
			return err
		}
	}
	encPriv, _, _, err := yardkey.EnsureEnc(cfg.PeerEncKey())
	if err != nil {
		return err
	}
	caller := &yards.Caller{Key: key, Enc: encPriv, HTTP: pairHTTP}
	accept, peerp := a.pairAccept(ctx, st, inv, addr, opt.name, opt.form != nil, yardrAt(cfg.Home), enrollment)
	called, err := caller.PairCall(ctx, addr, clean, inv.Name, accept)
	if errors.Is(err, errNotAdded) {
		return errors.New(strings.TrimPrefix(err.Error(), errNotAdded.Error()+": ") + nothingSaved)
	} else if err != nil {
		return notCalled{a.callFailed(ctx, cfg, st, addr, err)}
	}
	peer, host := *peerp, called.Host
	// refuse ends the pairing from this side: the other yard is told, so
	// its operator is not left to wait, and this yard logs why.
	refuse := func(reason string) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = caller.PairAnswer(ctx, called, false)
		_ = st.PairRefused(ctx, reason, host.Name, who)
	}

	if opt.form == nil {
		a.progress(w, warn, "%s answered at %s (it calls itself %s). Its key: %s", peer.Name, addr, host.Name, host.Key)
		if host.URL != addr {
			a.progress(w, warn, "it gives its address as %s; this yard keeps the one it answered at", host.URL)
		}
	}
	if existing != nil {
		a.enrollRename(w, warn, existing.Name, existing.Calls, host.Name)
	}
	previous := ""
	if existing != nil {
		previous = existing.Calls
	}
	yes, err := a.confirmCheck(w, warn, opt, peer.Name, host.Name, previous, called.Check())
	if err == nil && yes && opt.form != nil {
		// As on the yard that is called: every question before this yard's yes.
		var name string
		who, ticked, lerr := exposables(ctx, st)
		if err = lerr; err == nil {
			name, err = opt.form.ask(w, host.Name, who, ticked, func(name string) error {
				return st.CheckPeer(ctx, store.Peer{Name: name, Key: host.Key})
			})
		}
		if yes = !errors.Is(err, huh.ErrUserAborted); !yes {
			err = nil
		}
		peer.Name = or(name, peer.Name)
	}
	if err != nil || !yes || ctx.Err() != nil {
		refuse(store.Declined)
		if err != nil {
			return err
		}
		return errSaidNo
	}
	if opt.form != nil {
		fmt.Fprintln(w, "\nWaiting for the other operator to answer theirs.")
	} else {
		a.progress(w, warn, "waiting for %s's operator to confirm", peer.Name)
	}
	wait, cancel := context.WithTimeout(ctx, store.ConfirmTTL)
	defer cancel()
	out, outProof, err := caller.PairWaitWithProof(wait, called, pairPoll.caller)
	switch {
	case ctx.Err() != nil:
		refuse(store.Closed)
		if existing != nil {
			return peerEnrollmentPartial(peer.Name, "the caller stopped waiting for the final confirmed pairing answer")
		}
		return errors.New("the pairing was ended" + nothingSaved)
	case wait.Err() != nil:
		refuse(store.TimedOut)
		if existing != nil {
			return peerEnrollmentPartial(peer.Name, "the caller did not receive a final confirmed pairing answer")
		}
		return fmt.Errorf("%s's operator did not confirm within %s%s", peer.Name, short(store.ConfirmTTL), nothingSaved)
	case err != nil:
		if existing != nil {
			return peerEnrollmentPartial(peer.Name, "the confirmed pairing answer may have been saved at only one yard")
		}
		// The other yard may have saved this one: its answer did not arrive.
		return fmt.Errorf("asking %s how the pairing stands: %w\nThis yard saved nothing. If %s shows this yard as a peer, remove it there (`yardr peer rm` with the name it shows for this yard) and pair again", peer.Name, err, peer.Name)
	case out.State != store.Confirmed:
		return a.hostSaidNo(ctx, st, peer.Name, host.Name, out.Reason)
	}
	var saved *store.Peer
	if existing != nil {
		proof := store.PeerEnrollmentProof{Expected: *existing, Invite: enrollment.Invite, Transcript: called.Transcript,
			ConfirmRequestURL: outProof.RequestURL, ConfirmRequestTime: outProof.RequestTime, ConfirmRequestBody: outProof.RequestBody, ConfirmRequestSig: outProof.RequestSig,
			ConfirmResponseStatus: outProof.ResponseStatus, ConfirmResponseBody: outProof.ResponseBody, ConfirmResponseSig: outProof.ResponseSig}
		saved, err = st.CommitPeerEnrollment(ctx, proof, who)
	} else {
		saved, err = st.PairPeer(ctx, peer, who)
	}
	if err != nil {
		if existing != nil {
			return peerEnrollmentPartial(peer.Name, "the other yard confirmed; this yard refused its local compare-and-set")
		}
		return fmt.Errorf("%s has saved this yard, and this yard could not save it: %w\nRemove this yard there (`yardr peer rm` with the name it shows for this yard) and pair again", peer.Name, err)
	}
	if existing != nil {
		return a.pairEnrollmentDone(w, *saved, existing.Enc != "")
	}
	return a.pairDone(w, warn, ctx, cfg, st, paired{Role: "joiner", Peer: saved, Check: called.Check()}, opt)
}
