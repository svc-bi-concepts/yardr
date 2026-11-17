package store_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/yardkey"
)

func enrollmentFixture(t *testing.T) (*peerYard, store.PeerEnrollmentProof) {
	t.Helper()
	return enrollmentFixtureNamed(t, "south")
}

func enrollmentFixtureNamed(t *testing.T, name string) (*peerYard, store.PeerEnrollmentProof) {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".south")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	api, own := storetest.NewOwnedIn(t, home)
	y := newPeerYardOn(t, "http://north.test", api, own)
	if err := y.api.SetSetting(y.ctx, store.OperatorSetting, "jo", "jo"); err != nil {
		t.Fatal(err)
	}
	if err := y.api.DeletePeer(y.ctx, "north", "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := y.api.AddPeer(y.ctx, store.Peer{Name: "north", Key: y.key, Calls: "the-north", URL: "http://north.test"}, "jo"); err != nil {
		t.Fatal(err)
	}
	if name != "" {
		if err := y.api.SetSetting(y.ctx, "name", name, "jo"); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	_, remoteEnc, err := store.GenerateEnc()
	if err != nil {
		t.Fatal(err)
	}
	invite := store.Invite{Name: "the-north", Key: y.key, Enc: remoteEnc, URL: expected.URL}
	return y, enrollmentProof(t, y, *expected, invite)
}

func enrollmentProof(t *testing.T, y *peerYard, expected store.Peer, invite store.Invite) store.PeerEnrollmentProof {
	t.Helper()
	return enrollmentProofNamed(t, y, expected, invite, "south")
}

func enrollmentProofNamed(t *testing.T, y *peerYard, expected store.Peer, invite store.Invite, name string) store.PeerEnrollmentProof {
	t.Helper()
	joiner := store.Party{Nonce: store.NewNonce(), Name: name, Key: y.ownSign, Enc: y.ownEnc}
	host := store.Party{Nonce: store.NewNonce(), Name: invite.Name, Key: invite.Key, Enc: invite.Enc, URL: invite.URL}
	transcript := store.Transcript{Joiner: joiner, Host: host}
	transcript.Commit = store.Commit(joiner)
	requestBody, err := json.Marshal(store.PairingAnswer{Session: transcript.Session(), Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	requestTime, requestSig := yardkey.SignRequest(y.ownPriv, y.key, http.MethodPost, "/peers/pair/confirm", time.Now(), requestBody)
	responseBody, err := json.Marshal(store.PairingOutcome{State: store.Confirmed})
	if err != nil {
		t.Fatal(err)
	}
	return store.PeerEnrollmentProof{Expected: expected, Invite: invite, Transcript: transcript,
		ConfirmRequestURL:  expected.URL + "/peers/pair/confirm",
		ConfirmRequestTime: requestTime, ConfirmRequestBody: requestBody, ConfirmRequestSig: requestSig,
		ConfirmResponseStatus: http.StatusOK, ConfirmResponseBody: responseBody,
		ConfirmResponseSig: store.SignResponse(ed25519.PrivateKey(y.north), http.StatusOK, requestSig, responseBody)}
}

func TestCommitPeerEnrollmentEffectiveName(t *testing.T) {
	for _, name := range []string{"", "renamed"} {
		t.Run("setting="+name, func(t *testing.T) {
			y, proof := enrollmentFixtureNamed(t, name)
			effective := "south"
			if name != "" {
				effective = "renamed"
			}
			wrong := enrollmentProofNamed(t, y, proof.Expected, proof.Invite, "impostor")
			if _, err := y.api.CommitPeerEnrollment(y.ctx, wrong, "jo"); err == nil {
				t.Fatal("enrollment accepted another yard's name")
			}
			proof = enrollmentProofNamed(t, y, proof.Expected, proof.Invite, effective)
			got, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo")
			if err != nil || got.Enc != proof.Invite.Enc {
				t.Fatalf("enrollment with effective name = %+v, %v", got, err)
			}
		})
	}
}

func TestCommitPeerEnrollmentPreservesExistingRecordAndOutbox(t *testing.T) {
	y, proof := enrollmentFixture(t)
	if err := y.api.PutDepot(y.ctx, store.Depot{Name: "web", Base: "origin/main"}, "jo"); err != nil {
		t.Fatal(err)
	}
	putNamed(t, y.api, "incoming", incoming)
	if _, err := y.api.AllowPeer(y.ctx, "north", store.PeerAllow{Send: []string{store.PeerPing, store.PeerMail, store.PeerBead}, Receive: []string{store.PeerPing, store.PeerMail, store.PeerBead}}, "jo"); err != nil {
		t.Fatal(err)
	}
	cursorBody, err := json.Marshal(store.PeerPage{Messages: []store.PeerMessage{{ID: "cursor-7", Seq: 7, Kind: store.PeerPing, Body: json.RawMessage(`{"cursor":7}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := y.api.PeerPulled(y.ctx, "north", y.sealedBatch(t, ed25519.PrivateKey(y.north), y.key, cursorBody, nil)); err != nil {
		t.Fatalf("seed peer cursor: %v", err)
	}
	if _, err := y.api.SetPeerIntake(y.ctx, "north", &store.PeerIntake{Depot: "web", Flow: "incoming"}, "jo"); err != nil {
		t.Fatal(err)
	}
	putNamed(t, y.api, "away", away)
	if err := y.api.SetFlowPointer(y.ctx, "web", "", "away", "jo"); err != nil {
		t.Fatal(err)
	}
	if err := y.api.PutGroup(y.ctx, store.Group{Name: "at-north", Runner: store.PeerRunner, Limit: 2, Config: map[string]any{"peer": "north"}}, "jo"); err != nil {
		t.Fatal(err)
	}
	if got := y.take(t, beadMsg("b1", "web-1", nil)); got[0].Refused != "" {
		t.Fatalf("seed incoming custody: %+v", got)
	}
	incoming := y.arrived(t)
	// Receiving the same message again leaves its seen/dedup entry effective.
	duplicate := beadMsg("b1", "web-1", nil)
	askSeq, err := y.api.AddAsk(y.ctx, incoming.ID, store.Ask{To: "jo", Question: "Keep this open?", Context: []string{"Continuity check"}}, "jo")
	if err != nil {
		t.Fatal(err)
	}
	askBefore, err := y.api.UserAsks(y.ctx, "jo", "jo")
	if err != nil || len(askBefore.Open) == 0 || askBefore.Open[len(askBefore.Open)-1].Ask != askSeq {
		t.Fatalf("seed open ask: %+v, %v", askBefore, err)
	}
	incomingBefore, err := y.api.GetBead(y.ctx, incoming.ID)
	if err != nil {
		t.Fatal(err)
	}
	outgoingCreated, outgoingSession := y.send(t, store.NewBead{Title: "in-flight peer work", Priority: 1})
	outgoingBefore, err := y.api.GetBead(y.ctx, outgoingCreated.ID)
	if err != nil {
		t.Fatal(err)
	}
	outgoingSession, err = y.api.GetSession(y.ctx, outgoingSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := y.api.PeerSend(y.ctx, "north", store.PeerPing, json.RawMessage(`{"alive":true}`), "jo")
	if err != nil {
		t.Fatal(err)
	}
	if err := y.api.PeerSendFailed(y.ctx, "north", queued.Seq, "temporarily offline", time.Now().Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	before, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	outboxBefore, err := y.api.PeerOutbox(y.ctx, "north", 0)
	if err != nil {
		t.Fatal(err)
	}
	proof.Expected = *before
	got, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo")
	if err != nil {
		t.Fatal(err)
	}
	before.Enc = proof.Invite.Enc
	if !reflect.DeepEqual(got, before) {
		t.Fatalf("peer changed beyond its encryption pin:\nwant=%+v\ngot=%+v", before, got)
	}
	outboxAfter, err := y.api.PeerOutbox(y.ctx, "north", 0)
	if err != nil || !reflect.DeepEqual(outboxAfter, outboxBefore) {
		t.Fatalf("outbox changed during enrollment: before=%+v after=%+v err=%v", outboxBefore, outboxAfter, err)
	}
	incomingAfter, err := y.api.GetBead(y.ctx, incomingBefore.ID)
	if err != nil || !reflect.DeepEqual(incomingAfter, incomingBefore) {
		t.Fatalf("accepted incoming bead changed during enrollment: before=%+v after=%+v err=%v", incomingBefore, incomingAfter, err)
	}
	outgoingAfter, err := y.api.GetBead(y.ctx, outgoingBefore.ID)
	if err != nil || !reflect.DeepEqual(outgoingAfter, outgoingBefore) {
		t.Fatalf("in-flight outgoing bead changed during enrollment: before=%+v after=%+v err=%v", outgoingBefore, outgoingAfter, err)
	}
	sessionAfter, err := y.api.GetSession(y.ctx, outgoingSession.ID)
	if err != nil || !reflect.DeepEqual(sessionAfter, outgoingSession) {
		t.Fatalf("outgoing session changed during enrollment: before=%+v after=%+v err=%v", outgoingSession, sessionAfter, err)
	}
	if got := y.take(t, duplicate); !got[0].Duplicate {
		t.Fatalf("dedup state changed during enrollment: %+v", got)
	}
	askAfter, err := y.api.UserAsks(y.ctx, "jo", "jo")
	if err != nil || !reflect.DeepEqual(askAfter, askBefore) {
		t.Fatalf("open ask changed during enrollment: before=%+v after=%+v err=%v", askBefore, askAfter, err)
	}
	events, err := y.own.Events(y.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1]; got.Kind != "peer_encryption_enrolled" || got.Data["peer"] != "north" {
		t.Fatalf("enrollment audit event = %+v", got)
	}
	// A concurrent local grant change makes the confirmed local compare-and-set
	// fail without undoing the remote pin or disturbing queued custody.
	current, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	staleProof := enrollmentProof(t, y, *current, proof.Invite)
	if _, err := y.api.AllowPeer(y.ctx, "north", store.PeerAllow{Send: append(append([]string(nil), current.Send...), store.PeerFile), Receive: current.Receive}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := y.api.CommitPeerEnrollment(y.ctx, staleProof, "jo"); err == nil || !strings.Contains(err.Error(), "changed during enrollment") {
		t.Fatalf("concurrent compare-and-set error = %v", err)
	}
	if out, err := y.api.PeerOutbox(y.ctx, "north", 0); err != nil || !reflect.DeepEqual(out, outboxBefore) {
		t.Fatalf("outbox changed after partial completion: before=%+v after=%+v err=%v", outboxBefore, out, err)
	}
	askAfterPartial, err := y.api.UserAsks(y.ctx, "jo", "jo")
	if err != nil || !reflect.DeepEqual(askAfterPartial, askBefore) {
		t.Fatalf("ask changed after partial completion: before=%+v after=%+v err=%v", askBefore, askAfterPartial, err)
	}
	// A fresh, fully proven equal-pin retry is read-only and emits no event.
	current, err = y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	proof = enrollmentProof(t, y, *current, proof.Invite)
	events, err = y.own.Events(y.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore := len(events)
	got, err = y.api.CommitPeerEnrollment(y.ctx, proof, "jo")
	if err != nil || got.Enc != proof.Invite.Enc {
		t.Fatalf("equal-pin retry = %+v, %v", got, err)
	}
	events, err = y.own.Events(y.ctx, "", 0)
	if err != nil || len(events) != eventsBefore {
		t.Fatalf("equal-pin retry wrote an event: count %d -> %d, err %v", eventsBefore, len(events), err)
	}
}

func TestCommitPeerEnrollmentRefusesInvalidProofAndRotation(t *testing.T) {
	y, proof := enrollmentFixture(t)
	for _, tc := range []struct {
		name string
		edit func(*store.PeerEnrollmentProof)
		want string
	}{
		{"bad request signature", func(p *store.PeerEnrollmentProof) { p.ConfirmRequestSig = "bad" }, "confirmation request"},
		{"stale request", func(p *store.PeerEnrollmentProof) {
			stale := time.Now().Add(-10 * time.Minute)
			p.ConfirmRequestTime, p.ConfirmRequestSig = yardkey.SignRequest(y.ownPriv, y.key, http.MethodPost, "/peers/pair/confirm", stale, p.ConfirmRequestBody)
			p.ConfirmResponseSig = store.SignResponse(ed25519.PrivateKey(y.north), http.StatusOK, p.ConfirmRequestSig, p.ConfirmResponseBody)
		}, "timestamp"},
		{"cross transcript", func(p *store.PeerEnrollmentProof) { p.Transcript.Joiner.Nonce = store.NewNonce() }, "does not name the transcript"},
		{"bad response signature", func(p *store.PeerEnrollmentProof) { p.ConfirmResponseSig = "bad" }, "not signed"},
		{"unconfirmed", func(p *store.PeerEnrollmentProof) {
			p.ConfirmResponseBody, _ = json.Marshal(store.PairingOutcome{State: store.Ended})
			p.ConfirmResponseSig = store.SignResponse(ed25519.PrivateKey(y.north), http.StatusOK, p.ConfirmRequestSig, p.ConfirmResponseBody)
		}, "not confirmed"},
		{"signing identity mismatch", func(p *store.PeerEnrollmentProof) {
			p.Invite.Key = store.Public(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize)))
		}, "identity or authority"},
		{"malformed encryption identity", func(p *store.PeerEnrollmentProof) { p.Invite.Enc = "not-an-encryption-key" }, "valid current sealed-wire invite"},
		{"stale invite authority", func(p *store.PeerEnrollmentProof) { p.Invite.URL = "http://stale.example" }, "identity or authority"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := proof
			bad.ConfirmRequestBody = append([]byte(nil), proof.ConfirmRequestBody...)
			bad.ConfirmResponseBody = append([]byte(nil), proof.ConfirmResponseBody...)
			tc.edit(&bad)
			if _, err := y.api.CommitPeerEnrollment(y.ctx, bad, "jo"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CommitPeerEnrollment error = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := y.api.CommitPeerEnrollment(y.ctx, proof, "not-operator"); err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("nonoperator enrollment error = %v", err)
	}
	if _, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo"); err != nil {
		t.Fatal(err)
	}
	proof.Expected.Send = append(proof.Expected.Send, store.PeerMail)
	if _, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo"); err == nil || !strings.Contains(err.Error(), "changed during enrollment") {
		t.Fatalf("stale snapshot error = %v", err)
	}
	// A fresh proof with the current full row but a different proposed pin is refused.
	current, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	proof.Expected = *current
	_, otherEnc, err := store.GenerateEnc()
	if err != nil {
		t.Fatal(err)
	}
	proof = enrollmentProof(t, y, *current, store.Invite{Name: "the-north", Key: y.key, Enc: otherEnc, URL: current.URL})
	if _, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo"); err == nil || !strings.Contains(err.Error(), "different encryption key") {
		t.Fatalf("rotation error = %v", err)
	}
}

// A peer that renamed itself since it was linked keeps its key, encryption
// key, URL and link settings: the enrollment takes the new name from the
// invite the pairing confirmed, and nothing else changes.
func TestCommitPeerEnrollmentAcceptsARename(t *testing.T) {
	y, proof := enrollmentFixture(t)
	if _, err := y.api.AllowPeer(y.ctx, "north", store.PeerAllow{Send: []string{store.PeerPing, store.PeerMail}, Receive: []string{store.PeerPing, store.PeerMail}}, "jo"); err != nil {
		t.Fatal(err)
	}
	before, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	renamed := proof.Invite
	renamed.Name = "the-north-renamed"
	bad := []struct {
		name string
		edit func(*store.PeerEnrollmentProof)
		want string
	}{
		{"new signing key", func(p *store.PeerEnrollmentProof) {
			p.Invite.Key = store.Public(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize)))
		}, "identity or authority"},
		{"new URL", func(p *store.PeerEnrollmentProof) { p.Invite.URL = "http://elsewhere.test" }, "identity or authority"},
		{"pairing names another yard", func(p *store.PeerEnrollmentProof) { p.Invite.Name = "someone-else" }, "identity or authority"},
		{"rename not confirmed", func(p *store.PeerEnrollmentProof) {
			p.ConfirmResponseBody, _ = json.Marshal(store.PairingOutcome{State: store.Ended})
			p.ConfirmResponseSig = store.SignResponse(ed25519.PrivateKey(y.north), http.StatusOK, p.ConfirmRequestSig, p.ConfirmResponseBody)
		}, "not confirmed"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			p := enrollmentProof(t, y, *before, renamed)
			tc.edit(&p)
			if _, err := y.api.CommitPeerEnrollment(y.ctx, p, "jo"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CommitPeerEnrollment error = %v, want %q", err, tc.want)
			}
		})
	}
	after, err := y.api.GetPeer(y.ctx, "north")
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("refused renames changed the peer: before=%+v after=%+v err=%v", before, after, err)
	}
	proof = enrollmentProof(t, y, *before, renamed)
	got, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo")
	if err != nil {
		t.Fatalf("rename enrollment: %v", err)
	}
	if got.Calls != "the-north-renamed" || got.Key != before.Key || got.URL != before.URL || got.Enc != proof.Invite.Enc || got.Name != "north" {
		t.Fatalf("rename enrollment = %+v, before %+v", got, before)
	}
	if !reflect.DeepEqual(got.Send, before.Send) || !reflect.DeepEqual(got.Receive, before.Receive) {
		t.Fatalf("rename dropped link settings: before %+v after %+v", before, got)
	}
	// The retry with the renamed record is read-only, as an equal pin is.
	current, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	events, err := y.own.Events(y.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	proof = enrollmentProof(t, y, *current, renamed)
	if _, err := y.api.CommitPeerEnrollment(y.ctx, proof, "jo"); err != nil {
		t.Fatalf("renamed retry: %v", err)
	}
	after2, err := y.own.Events(y.ctx, "", 0)
	if err != nil || len(after2) != len(events) {
		t.Fatalf("renamed retry wrote an event: %d -> %d, err %v", len(events), len(after2), err)
	}
}

// eventsSince returns the events written after seq, by kind and data.
func eventsSince(t *testing.T, y *peerYard, seq uint64) []store.Event {
	t.Helper()
	all, err := y.own.Events(y.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.Event
	for _, e := range all {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}

// TestCommitPeerEnrollmentAuditsTheRenameMinimally pins the audit contract:
// a name change writes one peer_identity_enrolled with the local alias, the
// old and new names and whether the pin was added, and nothing of the proof;
// a pin filled with the name unchanged keeps peer_encryption_enrolled.
func TestCommitPeerEnrollmentAuditsTheRenameMinimally(t *testing.T) {
	y, proof := enrollmentFixture(t)
	before, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	renamed := proof.Invite
	renamed.Name = "the-north-renamed"
	mark := func() uint64 {
		all, err := y.own.Events(y.ctx, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) == 0 {
			return 0
		}
		return all[len(all)-1].Seq
	}
	seq := mark()
	if _, err := y.api.CommitPeerEnrollment(y.ctx, enrollmentProof(t, y, *before, renamed), "jo"); err != nil {
		t.Fatalf("rename enrollment: %v", err)
	}
	got := eventsSince(t, y, seq)
	if len(got) != 1 || got[0].Kind != "peer_identity_enrolled" {
		t.Fatalf("rename wrote %+v, want one peer_identity_enrolled", got)
	}
	want := map[string]any{"peer": "north", "old_calls": "the-north", "new_calls": "the-north-renamed", "encryption_pin_added": true}
	if !reflect.DeepEqual(got[0].Data, want) {
		t.Fatalf("rename audit data = %v, want %v", got[0].Data, want)
	}

	// The pin is now held and the name is current: a repeat writes nothing.
	seq = mark()
	current, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := y.api.CommitPeerEnrollment(y.ctx, enrollmentProof(t, y, *current, renamed), "jo"); err != nil {
		t.Fatalf("renamed retry: %v", err)
	}
	if got := eventsSince(t, y, seq); len(got) != 0 {
		t.Fatalf("unchanged repeat wrote %+v", got)
	}
}

// TestCommitPeerEnrollmentPinOnlyKeepsItsEvent: a pin filled under the name
// already saved is still audited as the encryption enrollment it always was.
func TestCommitPeerEnrollmentPinOnlyKeepsItsEvent(t *testing.T) {
	y, proof := enrollmentFixture(t)
	before, err := y.api.GetPeer(y.ctx, "north")
	if err != nil {
		t.Fatal(err)
	}
	all, err := y.own.Events(y.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	seq := all[len(all)-1].Seq
	if _, err := y.api.CommitPeerEnrollment(y.ctx, enrollmentProof(t, y, *before, proof.Invite), "jo"); err != nil {
		t.Fatalf("pin enrollment: %v", err)
	}
	got := eventsSince(t, y, seq)
	if len(got) != 1 || got[0].Kind != "peer_encryption_enrolled" || !reflect.DeepEqual(got[0].Data, map[string]any{"peer": "north", "key": y.key}) {
		t.Fatalf("pin wrote %+v, want one peer_encryption_enrolled for north", got)
	}
}
