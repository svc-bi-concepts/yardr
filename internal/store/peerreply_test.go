package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/yardkey"
)

// pair is two real yards linked both ways by mail: a, which sends the
// original mail from jo to ada on b, and b, which takes it. Each names the
// other by its key, so a reply carries a key the origin checks.
type pair struct {
	ctx        context.Context
	apiA, apiB store.API
	a, b       *store.Store
	keyA, keyB string
}

// newKey is the public key of a fresh yard identity.
func newKey(t *testing.T) string {
	t.Helper()
	k, _, err := yardkey.Ensure(filepath.Join(t.TempDir(), "yard.key"))
	if err != nil {
		t.Fatal(err)
	}
	return store.Public(k)
}

// oneYard is a yard with the operator ada and the users ada and jo.
func oneYard(t *testing.T) (store.API, *store.Store) {
	t.Helper()
	api, own := storetest.NewOwned(t)
	ctx := context.Background()
	if _, err := api.AddUser(ctx, "ada", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := api.AddUser(ctx, "jo", "ada"); err != nil {
		t.Fatal(err)
	}
	if err := api.SetSetting(ctx, store.OperatorSetting, "ada", "ada"); err != nil {
		t.Fatal(err)
	}
	return api, own
}

// linkedPair sets up the two yards. a exposes jo to b, b exposes ada to a,
// and each sends and takes mail over the link.
func linkedPair(t *testing.T) pair {
	t.Helper()
	ctx := context.Background()
	p := pair{ctx: ctx, keyA: newKey(t), keyB: newKey(t)}
	apiA, a := oneYard(t)
	apiB, b := oneYard(t)
	p.apiA, p.apiB, p.a, p.b = apiA, apiB, a, b
	_, encA, err := store.GenerateEnc()
	if err != nil {
		t.Fatal(err)
	}
	_, encB, err := store.GenerateEnc()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apiA.AddUser(ctx, "al", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := apiA.AddPeer(ctx, store.Peer{Name: "b", Key: p.keyB, Enc: encB}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := apiA.AllowPeer(ctx, "b", store.PeerAllow{Send: []string{"mail"}, Receive: []string{"mail"}, Expose: []string{"jo", "al"}}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := apiB.AddPeer(ctx, store.Peer{Name: "a", Key: p.keyA, Enc: encA}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := apiB.AllowPeer(ctx, "a", store.PeerAllow{Send: []string{"mail"}, Receive: []string{"mail"}, Expose: []string{"ada"}}, "ada"); err != nil {
		t.Fatal(err)
	}
	return p
}

// send queues jo's mail to ada on b from a, and returns its receipt and
// its message, which is still in a's outbox.
func (p pair) send(t *testing.T, text string) (store.MailReceipt, store.PeerOut) {
	t.Helper()
	r, err := p.a.MailPeerReceipt(p.ctx, "ada", "b", text, "jo")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.a.PeerOutbox(p.ctx, "b", 0)
	if err != nil || len(out) == 0 || out[len(out)-1].ID != r.ID {
		t.Fatalf("outbox = %+v, %v; want %s", out, err, r.ID)
	}
	return r, out[len(out)-1]
}

// deliver takes m on b, as a's link carries it, and returns what b said.
func (p pair) deliver(t *testing.T, m store.PeerOut) store.PeerTaken {
	t.Helper()
	got, err := p.b.PeerReceive(p.ctx, "a", p.keyA, []store.PeerMessage{m.PeerMessage})
	if err != nil || len(got) != 1 {
		t.Fatalf("b took %+v, %v", got, err)
	}
	return got[0]
}

// replyMsg is a mail of kind mail with the wire id re, from the name from
// to the name to, as the peer's yardr would send it.
func replyMsg(t *testing.T, id, re, from, to string) store.PeerMessage {
	t.Helper()
	body, err := json.Marshal(map[string]string{"to": to, "from": from, "text": "reply " + id})
	if err != nil {
		t.Fatal(err)
	}
	return store.PeerMessage{ID: id, Kind: store.PeerMail, Re: re, Body: body}
}

// mailOf is the mail event on s whose message id is id. It fails when there
// is none, or when there are two.
func mailOf(t *testing.T, s *store.Store, id string) store.Event {
	t.Helper()
	evs, err := s.Events(context.Background(), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []store.Event
	for _, e := range evs {
		if e.Kind == "mail" && e.Data["id"] == id {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("mail %s: %d events, want 1", id, len(found))
	}
	return found[0]
}

// relatedTo is the seq the mail event says it answers (in_reply_to), and
// whether it says so at all. Events read back from the log hold numbers as
// float64.
func relatedTo(e store.Event) (uint64, bool) {
	v, ok := e.Data["in_reply_to"]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return uint64(f), true
}

// TestReplyRelatesToTheOriginalMail is the request and reply across two
// real yards: the reply on the origin names its own original event, not the
// receiver's seq for the same mail, and it still does once the origin's
// acknowledged outbox message is gone.
func TestReplyRelatesToTheOriginalMail(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	if err := p.b.Mail(p.ctx, "ada", "filler", "jo"); err != nil {
		t.Fatal(err)
	}
	if got := p.deliver(t, out); got.Refused != "" || got.Duplicate {
		t.Fatalf("b refused the mail: %+v", got)
	}
	inbox := mailOf(t, p.b, out.ID)
	if inbox.Seq == r.Seq {
		t.Fatalf("both yards number the mail %d: the test must hold the namespaces apart", r.Seq)
	}
	if err := p.a.PeerDelivered(p.ctx, "b", out.Seq); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.a.PeerOutbox(p.ctx, "b", 0); len(got) != 0 {
		t.Fatalf("outbox after delivery = %+v", got)
	}

	reply := replyMsg(t, "reply-1", out.ID, "ada", "jo")
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{reply})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	e := mailOf(t, p.a, "reply-1")
	seq, ok := relatedTo(e)
	if !ok || seq != r.Seq {
		t.Fatalf("in_reply_to = %v (%v), want the original event %d", e.Data["in_reply_to"], ok, r.Seq)
	}
	if e.Data["re"] != out.ID {
		t.Fatalf("re = %v, want the wire id %s", e.Data["re"], out.ID)
	}
	if seq == inbox.Seq {
		t.Fatalf("in_reply_to took b's seq %d", inbox.Seq)
	}
}

// TestUnvalidatedReplyStaysUncorrelated takes replies that name no mail
// this yard can check, or check it and find it does not match. Each is
// accepted as ordinary mail, with its re kept as what it says and no
// in_reply_to.
func TestUnvalidatedReplyStaysUncorrelated(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	_, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	// A third yard, linked under a key that never queued the mail.
	keyC := newKey(t)
	if _, err := p.apiA.AddPeer(p.ctx, store.Peer{Name: "c", Key: keyC}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.apiA.AllowPeer(p.ctx, "c", store.PeerAllow{Receive: []string{"mail"}, Expose: []string{"jo"}}, "ada"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		peer string
		key  string
		msg  store.PeerMessage
	}{
		{"unknown wire id", "b", p.keyB, replyMsg(t, "u-1", "0123456789abcdef", "ada", "jo")},
		{"the sender is not the recipient", "b", p.keyB, replyMsg(t, "u-2", out.ID, "bo", "jo")},
		{"the recipient is not the sender", "b", p.keyB, replyMsg(t, "u-3", out.ID, "ada", "al")},
		{"another peer's key", "c", keyC, replyMsg(t, "u-4", out.ID, "ada", "jo")},
		{"no re at all", "b", p.keyB, replyMsg(t, "u-5", "", "ada", "jo")},
	}
	for _, c := range cases {
		got, err := p.a.PeerReceive(p.ctx, c.peer, c.key, []store.PeerMessage{c.msg})
		if err != nil || len(got) != 1 || got[0].Refused != "" {
			t.Fatalf("%s: a took %+v, %v; want accepted as ordinary mail", c.name, got, err)
		}
		e := mailOf(t, p.a, c.msg.ID)
		if _, ok := relatedTo(e); ok {
			t.Fatalf("%s: in_reply_to = %v, want none", c.name, e.Data["in_reply_to"])
		}
		if c.msg.Re == "" {
			if _, ok := e.Data["re"]; ok {
				t.Fatalf("%s: re = %v, want none", c.name, e.Data["re"])
			}
		} else if e.Data["re"] != c.msg.Re {
			t.Fatalf("%s: re = %v, want the wire id as sent", c.name, e.Data["re"])
		}
	}
}

// TestRelationFollowsARenamedLink holds the peer's key, not its name, as
// what identifies it: a link renamed keeps the relation, and a name made
// again under another key does not inherit it.
func TestRelationFollowsARenamedLink(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	if _, err := p.apiA.RenamePeer(p.ctx, "b", "bb", "ada"); err != nil {
		t.Fatal(err)
	}
	got, err := p.a.PeerReceive(p.ctx, "bb", p.keyB, []store.PeerMessage{replyMsg(t, "rn-1", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("renamed link: %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "rn-1")); !ok || seq != r.Seq {
		t.Fatalf("renamed link: in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}

	// The name b is made again, under another key: the mail it sends back
	// is not the one this yard queued for b.
	keyC := newKey(t)
	if err := p.apiA.DeletePeer(p.ctx, "bb", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.apiA.AddPeer(p.ctx, store.Peer{Name: "bb", Key: keyC}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.apiA.AllowPeer(p.ctx, "bb", store.PeerAllow{Receive: []string{"mail"}, Expose: []string{"jo"}}, "ada"); err != nil {
		t.Fatal(err)
	}
	got, err = p.a.PeerReceive(p.ctx, "bb", keyC, []store.PeerMessage{replyMsg(t, "rn-2", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("recreated name: %+v, %v", got, err)
	}
	if _, ok := relatedTo(mailOf(t, p.a, "rn-2")); ok {
		t.Fatalf("recreated name under another key inherits the relation")
	}
}

// TestDuplicateReplyAddsNoRelation is a transport retry of a reply: it is
// taken once, with one event, and that event keeps its relation.
func TestDuplicateReplyAddsNoRelation(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	reply := replyMsg(t, "dup-1", out.ID, "ada", "jo")
	for i := 0; i < 2; i++ {
		got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{reply})
		if err != nil || len(got) != 1 || got[0].Refused != "" {
			t.Fatalf("take %d: %+v, %v", i, got, err)
		}
		if i == 1 && !got[0].Duplicate {
			t.Fatalf("second take is not a duplicate: %+v", got[0])
		}
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "dup-1")); !ok || seq != r.Seq {
		t.Fatalf("surviving event: in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}
}

// lastSeq is the seq of the newest event on s.
func lastSeq(t *testing.T, s *store.Store) uint64 {
	t.Helper()
	evs, err := s.Events(context.Background(), "", 0)
	if err != nil || len(evs) == 0 {
		t.Fatalf("events = %d, %v", len(evs), err)
	}
	return evs[len(evs)-1].Seq
}

// TestReplyRelatesWhenBothYardsNumberTheMailAlike makes the two yards give
// the mail the same seq, the case that tells a namespace apart from the
// others: the relation must still be a's own, found by the wire id.
func TestReplyRelatesWhenBothYardsNumberTheMailAlike(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	for lastSeq(t, p.b) >= lastSeq(t, p.a) {
		if err := p.a.Mail(p.ctx, "ada", "filler", "jo"); err != nil {
			t.Fatal(err)
		}
	}
	for lastSeq(t, p.b)+1 < lastSeq(t, p.a) {
		if err := p.b.Mail(p.ctx, "ada", "filler", "jo"); err != nil {
			t.Fatal(err)
		}
	}
	r, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	if inbox := mailOf(t, p.b, out.ID); inbox.Seq != r.Seq {
		t.Fatalf("seqs differ (a %d, b %d): the test holds nothing apart", r.Seq, inbox.Seq)
	}
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "eq-1", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "eq-1")); !ok || seq != r.Seq {
		t.Fatalf("in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}
}

// TestConcurrentRepliesEachRelateToTheirOwnMail takes the replies to several
// mails at once: each names its own original, none another's.
func TestConcurrentRepliesEachRelateToTheirOwnMail(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	const n = 8
	seqs := map[string]uint64{}
	var wires []string
	for i := 0; i < n; i++ {
		r, out := p.send(t, "question "+string(rune('a'+i)))
		p.deliver(t, out)
		seqs[out.ID] = r.Seq
		wires = append(wires, out.ID)
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i, w := range wires {
		wg.Add(1)
		go func(i int, w string) {
			defer wg.Done()
			got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, fmt.Sprintf("cc-%d", i), w, "ada", "jo")})
			if err != nil || len(got) != 1 || got[0].Refused != "" {
				errs <- fmt.Errorf("reply %d: %+v, %v", i, got, err)
			}
		}(i, w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for i, w := range wires {
		if seq, ok := relatedTo(mailOf(t, p.a, fmt.Sprintf("cc-%d", i))); !ok || seq != seqs[w] {
			t.Fatalf("reply %d: in_reply_to = %d, %v; want %d", i, seq, ok, seqs[w])
		}
	}
}

// TestReverseDirectionRepliesAreChecked runs the exchange the other way: b
// sends and a answers, so the relation is b's own. A mail that names a wire
// id this yard received, not sent, relates to nothing.
func TestReverseDirectionRepliesAreChecked(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, err := p.b.MailPeerReceipt(p.ctx, "jo", "a", "ping", "ada")
	if err != nil {
		t.Fatal(err)
	}
	outB, err := p.b.PeerOutbox(p.ctx, "a", 0)
	if err != nil || len(outB) != 1 {
		t.Fatalf("b's outbox = %+v, %v", outB, err)
	}
	if got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{outB[0].PeerMessage}); err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	// a's answer goes back to b, with jo as sender and ada as recipient.
	got, err := p.b.PeerReceive(p.ctx, "a", p.keyA, []store.PeerMessage{replyMsg(t, "rv-1", outB[0].ID, "jo", "ada")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("b took %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.b, "rv-1")); !ok || seq != r.Seq {
		t.Fatalf("in_reply_to = %d, %v; want b's own %d", seq, ok, r.Seq)
	}
	// b names the mail it sent, to a, which only received it: a sent no
	// such mail, so there is nothing to relate.
	got, err = p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "rv-2", outB[0].ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	if e := mailOf(t, p.a, "rv-2"); e.Data["re"] != outB[0].ID {
		t.Fatalf("re = %v", e.Data["re"])
	} else if _, ok := relatedTo(e); ok {
		t.Fatalf("a mail the yard received relates: in_reply_to = %v", e.Data["in_reply_to"])
	}
}

// TestDelayedReplyStillRelates takes a reply after other mail has come and
// gone, once before the original is acknowledged and once after.
func TestDelayedReplyStillRelates(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "dl-1", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("early reply: %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "dl-1")); !ok || seq != r.Seq {
		t.Fatalf("early reply: in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}
	if err := p.a.PeerDelivered(p.ctx, "b", out.Seq); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := p.a.Mail(p.ctx, "ada", "later", "jo"); err != nil {
			t.Fatal(err)
		}
		_, o := p.send(t, "another question")
		if err := p.a.PeerDelivered(p.ctx, "b", o.Seq); err != nil {
			t.Fatal(err)
		}
	}
	got, err = p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "dl-2", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("late reply: %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "dl-2")); !ok || seq != r.Seq {
		t.Fatalf("late reply: in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}
}

// TestPlainMailKeepsOrdinaryDelivery is mail from a peer that names no mail
// at all, as an older yardr sends it: delivered with no relation, no re and
// the original's local data untouched.
func TestPlainMailKeepsOrdinaryDelivery(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	_, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "pl-1", "", "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" || got[0].Duplicate {
		t.Fatalf("a took %+v, %v", got, err)
	}
	e := mailOf(t, p.a, "pl-1")
	for _, k := range []string{"in_reply_to", "re", "answered"} {
		if v, ok := e.Data[k]; ok {
			t.Fatalf("plain mail carries %s = %v", k, v)
		}
	}
	if e.Data["text"] != "reply pl-1" {
		t.Fatalf("text = %v", e.Data["text"])
	}
}

// TestRelationSurvivesRemovingAndAddingTheSameKey holds identity to the key:
// a link removed and made again under the same key is the same peer, so the
// relation holds.
func TestRelationSurvivesRemovingAndAddingTheSameKey(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	p.deliver(t, out)
	if err := p.apiA.DeletePeer(p.ctx, "b", "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.apiA.AddPeer(p.ctx, store.Peer{Name: "b", Key: p.keyB}, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.apiA.AllowPeer(p.ctx, "b", store.PeerAllow{Receive: []string{"mail"}, Expose: []string{"jo"}}, "ada"); err != nil {
		t.Fatal(err)
	}
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{replyMsg(t, "rk-1", out.ID, "ada", "jo")})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	if seq, ok := relatedTo(mailOf(t, p.a, "rk-1")); !ok || seq != r.Seq {
		t.Fatalf("in_reply_to = %d, %v; want %d", seq, ok, r.Seq)
	}
}

// TestPeerReplyNamesTheMailItAnswers is the native reply across two real
// yards: b answers the mail it took by its own seq, and the reply goes back
// to a under the wire id a sent it, so a relates it to its own event.
func TestPeerReplyNamesTheMailItAnswers(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "when is the freeze?")
	if got := p.deliver(t, out); got.Refused != "" || got.Duplicate {
		t.Fatalf("b refused the mail: %+v", got)
	}
	inbox := mailOf(t, p.b, out.ID)

	rr, err := p.b.MailPeerReply(p.ctx, inbox.Seq, "friday", "ada")
	if err != nil {
		t.Fatal(err)
	}
	if rr.Direction != store.ReceiptOut || rr.Peer != "a" || rr.PeerKey != p.keyA || rr.From != "ada" || rr.To != "jo" {
		t.Fatalf("receipt = %+v", rr)
	}
	msgs, err := p.b.PeerOutbox(p.ctx, "a", 0)
	if err != nil || len(msgs) != 1 || msgs[0].ID != rr.ID || msgs[0].Re != out.ID {
		t.Fatalf("outbox = %+v, %v; want the reply %s naming re %s", msgs, err, rr.ID, out.ID)
	}
	got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{msgs[0].PeerMessage})
	if err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	e := mailOf(t, p.a, rr.ID)
	if seq, ok := relatedTo(e); !ok || seq != r.Seq {
		t.Fatalf("in_reply_to = %v (%v), want a's own event %d", e.Data["in_reply_to"], ok, r.Seq)
	}
	if e.Data["re"] != out.ID {
		t.Fatalf("re = %v, want the wire id %s", e.Data["re"], out.ID)
	}
}

// TestPeerReplyRefusesWhatItCannotAnswer holds the operation to the mail it
// may answer: only mail to the actor, from another yard, and an existing
// message. Each refusal queues nothing.
func TestPeerReplyRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	_, out := p.send(t, "q")
	p.deliver(t, out)
	inbox := mailOf(t, p.b, out.ID)
	local, err := p.b.MailLocalReceipt(p.ctx, "ada", "local", "jo")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, text, actor string
		seq               uint64
	}{
		{"not the receiver", "x", "jo", inbox.Seq},
		{"local mail", "x", "ada", local.Seq},
		{"empty text", " ", "ada", inbox.Seq},
		{"no such message", "x", "ada", 999999},
	}
	for _, c := range cases {
		if _, err := p.b.MailPeerReply(p.ctx, c.seq, c.text, c.actor); err == nil {
			t.Fatalf("%s: answered, want a refusal", c.name)
		}
	}
	if msgs, err := p.b.PeerOutbox(p.ctx, "a", 0); err != nil || len(msgs) != 0 {
		t.Fatalf("refusals queued %+v, %v", msgs, err)
	}
}

func TestPeerReplyRequiresEncryptionPin(t *testing.T) {
	t.Parallel()
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "received", true: "direct"}[direct], func(t *testing.T) {
			_, own := oneYard(t)
			ctx := context.Background()
			key := newKey(t)
			if _, err := own.AddPeer(ctx, store.Peer{Name: "north", Key: key}, "ada"); err != nil {
				t.Fatal(err)
			}
			if _, err := own.AllowPeer(ctx, "north", store.PeerAllow{Send: []string{"mail"}, Receive: []string{"mail"}, Expose: []string{"ada"}}, "ada"); err != nil {
				t.Fatal(err)
			}
			if direct {
				if err := own.MailFromPeer(ctx, "ada", "freeze?", "dispatcher", "north"); err != nil {
					t.Fatal(err)
				}
			} else {
				body, _ := json.Marshal(map[string]string{"to": "ada", "from": "dispatcher", "text": "freeze?"})
				got, err := own.PeerReceive(ctx, "north", key, []store.PeerMessage{{ID: "original", Kind: store.PeerMail, Body: body}})
				if err != nil || len(got) != 1 || got[0].Refused != "" {
					t.Fatalf("received mail = %+v, %v", got, err)
				}
			}
			box, err := own.UserInbox(ctx, "ada", 0, "ada")
			if err != nil || len(box.Mail) != 1 {
				t.Fatalf("inbox = %+v, %v", box, err)
			}
			if _, err := own.MailPeerReply(ctx, box.Mail[0].Seq, "Friday.", "ada"); err == nil || !strings.Contains(err.Error(), "link to north has no encryption key") {
				t.Fatalf("reply without a pin = %v", err)
			}
			if msgs, err := own.PeerOutbox(ctx, "north", 0); err != nil || len(msgs) != 0 {
				t.Fatalf("outbox after refused reply = %+v, %v", msgs, err)
			}
		})
	}
}

// TestReplyToPrunedMailStaysUncorrelated: a's event for its mail is pruned
// while the outbox still holds the message, which keeps the receipt. A reply
// that names the mail is accepted as ordinary mail: re is kept as sent, and
// no in_reply_to names the seq that is gone.
func TestReplyToPrunedMailStaysUncorrelated(t *testing.T) {
	t.Parallel()
	p := linkedPair(t)
	r, out := p.send(t, "held")
	if err := p.a.Mail(p.ctx, "al", "later", "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.a.Prune(p.ctx, store.Retention{Events: time.Nanosecond}, "ada"); err != nil {
		t.Fatal(err)
	}
	evs, err := p.a.Events(p.ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Seq == r.Seq {
			t.Fatalf("a's event %d survived the prune", r.Seq)
		}
	}
	if _, err := p.a.MailReceiptOf(p.ctx, r.Seq, "jo"); err != nil {
		t.Fatalf("custody lost the receipt: %v", err)
	}

	if got := p.deliver(t, out); got.Refused != "" {
		t.Fatalf("b refused the mail: %+v", got)
	}
	inbox := mailOf(t, p.b, out.ID)
	rr, err := p.b.MailPeerReply(p.ctx, inbox.Seq, "reply", "ada")
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := p.b.PeerOutbox(p.ctx, "a", 0)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("outbox = %+v, %v", msgs, err)
	}
	if got, err := p.a.PeerReceive(p.ctx, "b", p.keyB, []store.PeerMessage{msgs[0].PeerMessage}); err != nil || len(got) != 1 || got[0].Refused != "" {
		t.Fatalf("a took %+v, %v", got, err)
	}
	e := mailOf(t, p.a, rr.ID)
	if v, ok := e.Data["in_reply_to"]; ok {
		t.Fatalf("in_reply_to = %v, want none for a pruned event", v)
	}
	if e.Data["re"] != out.ID {
		t.Fatalf("re = %v, want %s kept as sent", e.Data["re"], out.ID)
	}
}
