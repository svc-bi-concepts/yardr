package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// CommitPeerEnrollment fills one missing pin, or recognizes an equal pin
// after a fresh signed pairing when the other yard committed first.
func (s *Store) CommitPeerEnrollment(ctx context.Context, proof PeerEnrollmentProof, actor string) (*Peer, error) {
	var out Peer
	err := s.write(func(w *wtx) error {
		if err := w.may(ctx, actor, ActPair, Object{Kind: KindPeer}); err != nil {
			return err
		}
		if err := s.verifyPeerEnrollmentProof(w.Tx, proof); err != nil {
			return err
		}
		var err error
		out, _, err = w.enrollPeerEncryption(proof.Expected, proof.Invite, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.GetPeer(ctx, out.Name)
}

// verifyPeerEnrollmentProof ties the caller's mutation to the exact native
// confirm exchange. The ordinary pair command has already shown the check;
// the store verifies that its signed confirmation names this transcript.
func (s *Store) verifyPeerEnrollmentProof(tx *bolt.Tx, p PeerEnrollmentProof) error {
	if len(p.ConfirmRequestURL) > 2048 || len(p.ConfirmRequestBody) > PairingMessageBound || len(p.ConfirmResponseBody) > PairingMessageBound ||
		len(p.ConfirmRequestSig) > 256 || len(p.ConfirmResponseSig) > 256 || len(p.ConfirmRequestTime) > 32 {
		return errors.New("peer enrollment proof exceeds the pairing message bound")
	}
	requestURL, err := url.Parse(p.ConfirmRequestURL)
	if err != nil || requestURL.String() != p.Expected.URL+"/peers/pair/confirm" || requestURL.RawQuery != "" || requestURL.Fragment != "" || requestURL.User != nil {
		return errors.New("peer enrollment confirmation did not use the unchanged peer URL")
	}
	if err := VerifyPairRequest(s.yardKey(), p.Expected.Key, http.MethodPost, "/peers/pair/confirm", p.ConfirmRequestTime, p.ConfirmRequestSig, p.ConfirmRequestBody, now()); err != nil {
		return fmt.Errorf("peer enrollment confirmation request: %w", err)
	}
	var answer PairingAnswer
	if err := json.Unmarshal(p.ConfirmRequestBody, &answer); err != nil || !answer.Confirm || answer.Session != p.Transcript.Session() {
		return errors.New("peer enrollment confirmation does not name the transcript")
	}
	if p.ConfirmResponseStatus != http.StatusOK || VerifyResponse(p.Expected.Key, p.ConfirmResponseStatus, p.ConfirmRequestSig, p.ConfirmResponseSig, p.ConfirmResponseBody) != nil {
		return errors.New("peer enrollment confirmation response is not signed by the peer")
	}
	var outcome PairingOutcome
	if err := json.Unmarshal(p.ConfirmResponseBody, &outcome); err != nil || outcome.State != Confirmed {
		return errors.New("peer enrollment confirmation is not confirmed")
	}
	return s.validateEnrollmentBinding(tx, p.Expected, p.Invite, p.Transcript)
}

func (s *Store) validateEnrollmentBinding(tx *bolt.Tx, expected Peer, invite Invite, transcript Transcript) error {
	parsed, err := ParseInvite(invite.String())
	if err != nil || invite.Enc == "" || parsed != invite {
		return errors.New("peer enrollment requires a valid current sealed-wire invite")
	}
	if err := transcript.Joiner.Valid(); err != nil {
		return fmt.Errorf("peer enrollment transcript: %w", err)
	}
	if err := transcript.Host.Valid(); err != nil {
		return fmt.Errorf("peer enrollment transcript: %w", err)
	}
	if Commit(transcript.Joiner) != transcript.Commit || transcript.Joiner.URL != "" {
		return errors.New("peer enrollment transcript does not match its commitment")
	}
	// The name may have changed since the peer was linked; the pinned key
	// and URL may not, and the pairing must name the invite's yard.
	if expected.Key != invite.Key || expected.URL != invite.URL ||
		transcript.Host.Name != invite.Name || transcript.Host.Key != invite.Key || transcript.Host.Enc != invite.Enc || transcript.Host.URL != invite.URL {
		return fmt.Errorf("peer %s identity or authority does not match the invite and pairing", expected.Name)
	}
	if expected.Enc != "" && expected.Enc != invite.Enc {
		return fmt.Errorf("peer %s already has a different encryption key; key rotation is not supported", expected.Name)
	}
	var current Peer
	ok, err := get(tx, bPeers, expected.Name, &current)
	if err != nil {
		return err
	}
	if !ok || !reflect.DeepEqual(current, enrollmentPeerRecord(expected)) {
		return fmt.Errorf("peer %s changed during enrollment", expected.Name)
	}
	var name string
	if _, err := get(tx, bSettings, "name", &name); err != nil {
		return err
	}
	if name == "" {
		name = strings.TrimLeft(filepath.Base(s.home), ".")
	}
	if transcript.Joiner.Name != name || transcript.Joiner.Key != s.yardKey() {
		return errors.New("peer enrollment transcript does not match this yard's identity")
	}
	priv, err := s.peerEncPriv()
	if err != nil {
		return errors.New("this yard has no usable encryption identity for the pairing")
	}
	ownEnc, err := EncPublicOf(priv)
	if err != nil || transcript.Joiner.Enc != ownEnc {
		return errors.New("peer enrollment transcript does not match this yard's encryption identity")
	}
	return nil
}

// enrollPeerEncryption pins the invite's encryption key on the peer and
// takes the name it calls itself from the invite: the key and URL were
// checked against the invite and the pairing before this, and a changed
// name is the one thing that may differ from the record.
func (w *wtx) enrollPeerEncryption(expected Peer, invite Invite, actor string) (Peer, bool, error) {
	var current Peer
	ok, err := get(w.Tx, bPeers, expected.Name, &current)
	if err != nil {
		return Peer{}, false, err
	}
	if !ok {
		return Peer{}, false, ErrNotFound
	}
	if !reflect.DeepEqual(current, enrollmentPeerRecord(expected)) {
		return Peer{}, false, fmt.Errorf("peer %s changed during enrollment", expected.Name)
	}
	enc := invite.Enc
	if current.Enc != "" && current.Enc != enc {
		return Peer{}, false, fmt.Errorf("peer %s already has a different encryption key; key rotation is not supported", expected.Name)
	}
	if current.Enc == enc && current.Calls == invite.Name {
		return current, true, nil
	}
	if current.Enc == "" {
		if _, err := ParseEncPublic(enc); err != nil {
			return Peer{}, false, fmt.Errorf("peer %s: %w", expected.Name, err)
		}
	}
	data := map[string]any{"peer": expected.Name, "key": expected.Key}
	if current.Calls != invite.Name {
		// The event keeps both names, so a rename the operators confirmed
		// is on the record as well as in the peer's current name.
		data["calls_from"], data["calls_to"] = current.Calls, invite.Name
	}
	current.Enc, current.Calls = enc, invite.Name
	if err := put(w.Tx, bPeers, expected.Name, &current); err != nil {
		return Peer{}, false, err
	}
	if err := w.event("", "peer_encryption_enrolled", actor, data); err != nil {
		return Peer{}, false, err
	}
	return current, false, nil
}

// enrollmentPeerRecord removes only read-time summaries added by GetPeer;
// the compare still covers every field stored with the peer record.
func enrollmentPeerRecord(p Peer) Peer {
	p.Roster = nil
	p.Waiting, p.Failed, p.Error = 0, 0, ""
	p.Away, p.Beads = 0, 0
	p.OpenByDepot, p.ArriveByDepot = nil, nil
	return p
}

// errOwnPeerInvite is the refusal shared by peer add and its run-ask card.
var errOwnPeerInvite = errors.New("that invite is this yard's own: give it to the other yard's operator, and add theirs here")

// AddPeer records another yard as a peer. The link carries nothing until
// AllowPeer says what. A name or a key that is a peer already is refused
// (peerFree). Who a yard is linked with, and what it takes from them, is
// the operator's to say (operatorOnly), here and in every change of a link
// that widens it: not for a crew member's session to grant. A run ask may
// propose it, for the operator to approve (runask.go).
func (s *Store) AddPeer(ctx context.Context, p Peer, actor string) (*Peer, error) {
	var out *Peer
	err := s.write(func(w *wtx) error {
		var err error
		out, err = w.addPeer(ctx, p, actor, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// addPeer is AddPeer inside the transaction, so that an approved run ask
// links the peer in the write that records its approval: by is then the
// ask it was approved on, which the event carries.
func (w *wtx) addPeer(ctx context.Context, p Peer, actor string, by *approvedOn) (*Peer, error) {
	// A new link carries nothing, whatever the caller holds.
	p = Peer{Name: p.Name, Key: p.Key, Enc: p.Enc, URL: p.URL, Calls: p.Calls, Send: []string{}, Receive: []string{}, AddedAt: now()}
	if err := w.checkPeerInvite(p.Key); err != nil {
		return nil, err
	}
	// Askable: the card names the peer, the yard and its key, and where
	// this yard calls it, and an approval that reaches the yard through
	// that peer's own reach is refused (approveAsk), so a peer cannot
	// add its own link.
	if err := w.may(ctx, actor, ActAddPeer, Object{Kind: KindPeer, Name: p.Name, Ask: Askable}); err != nil {
		return nil, err
	}
	if err := peerFree(w.Tx, &p); err != nil {
		return nil, err
	}
	p.Since = w.Bucket(bEvents).Sequence()
	if err := put(w.Tx, bPeers, p.Name, p); err != nil {
		return nil, err
	}
	data := map[string]any{"peer": p.Name, "key": p.Key}
	if p.URL != "" {
		data["url"] = p.URL
	}
	if by != nil {
		data["approved"] = by
	}
	if err := w.event("", "peer_added", actor, data); err != nil {
		return nil, err
	}
	// A link stands where none did: a hold written against its absence
	// (a peer add ask, which holds the name free) is stale in this write.
	if err := w.tellPeerStale(p.Name, nil, &p, actor); err != nil {
		return nil, err
	}
	return &p, nil
}

// checkPeerInvite refuses a link back to this yard, when the process that
// owns the store said where its own key is (SetYard).
func (w *wtx) checkPeerInvite(key string) error {
	if w.store != nil && key != "" && w.store.yardKey() == key {
		return errOwnPeerInvite
	}
	return nil
}
