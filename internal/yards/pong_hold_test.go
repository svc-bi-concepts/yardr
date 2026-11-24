package yards

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
	"yardr.dev/yardr/internal/yardkey"
)

// servedYard is a yard whose receiver is served over HTTP, set up as newYard
// is in yards_test.go, which cannot read the receiver's held pulls: this
// package can.
type servedYard struct {
	api    store.API
	own    *store.Store
	rc     *Receiver
	srv    *httptest.Server
	key    []byte // the yard's signing key
	enc    string // its public encryption key, which a peer seals to
	encKey []byte // its private encryption key
}

func newServedYard(t *testing.T) *servedYard {
	t.Helper()
	api, own := storetest.NewOwned(t)
	dir := t.TempDir()
	// The receiver reads its encryption key from peer-enc.key beside its
	// key file (encFileOf), so the two are named as it names them.
	keyFile := filepath.Join(dir, "peer.key")
	y := &servedYard{api: api, own: own}
	var err error
	if y.key, _, err = yardkey.Ensure(keyFile); err != nil {
		t.Fatal(err)
	}
	if y.encKey, y.enc, _, err = yardkey.EnsureEnc(filepath.Join(dir, "peer-enc.key")); err != nil {
		t.Fatal(err)
	}
	own.SetYard(keyFile, "test")
	y.rc = NewReceiver(own, keyFile, nil)
	y.srv = httptest.NewServer(y.rc.Handler())
	t.Cleanup(y.srv.Close)
	return y
}

// pongHoldFixture is yard a, which has no URL for its peer b, and b, which
// pulls from a. b asks a for what waits for it with a held question, as a
// peer that cannot be called does.
type pongHoldFixture struct {
	a, b    *servedYard
	toA     store.Peer // a, as b calls it
	bCaller *Caller    // b's side of the exchange
}

func newPongHoldFixture(t *testing.T) *pongHoldFixture {
	t.Helper()
	ctx := context.Background()
	a, b := newServedYard(t), newServedYard(t)
	if _, err := a.api.AddPeer(ctx, store.Peer{Name: "b", Key: store.Public(b.key), Enc: b.enc}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.api.AllowPeer(ctx, "b", store.PeerAllow{Send: []string{store.PeerPing}, Receive: []string{store.PeerPing}}, "jo"); err != nil {
		t.Fatal(err)
	}
	return &pongHoldFixture{
		a:       a,
		b:       b,
		toA:     store.Peer{Name: "a", Key: store.Public(a.key), Enc: a.enc, URL: a.srv.URL},
		bCaller: &Caller{Key: b.key, Enc: b.encKey, Now: time.Now},
	}
}

// pullHeld has b ask a with a held question and returns when a holds it, with
// the answer to that question on the channel.
func (f *pongHoldFixture) pullHeld(t *testing.T) <-chan pullAnswer {
	t.Helper()
	ctx := context.Background()
	answer := make(chan pullAnswer, 1)
	go func() {
		batch, err := f.bCaller.Pull(ctx, f.toA, 0, 30*time.Second)
		answer <- pullAnswer{batch: batch, err: err}
	}()
	fuse := heldFuse()
	for end := time.Now().Add(fuse); ; time.Sleep(time.Millisecond) {
		f.a.rc.mu.Lock()
		n := len(f.a.rc.held)
		f.a.rc.mu.Unlock()
		if n == 1 {
			return answer
		}
		if time.Now().After(end) {
			t.Fatalf("a did not hold b's pull within %s", fuse)
		}
	}
}

type pullAnswer struct {
	batch store.PeerBatch
	err   error
}

// awaitAnswer waits for the held question to be answered, and fails the test
// when it takes longer than a wake should: the hold is ended by the write
// that queued the message, so the answer comes within milliseconds.
func awaitAnswer(t *testing.T, answer <-chan pullAnswer, from time.Time) {
	t.Helper()
	const bound = 5 * time.Second
	select {
	case got := <-answer:
		if got.err != nil {
			t.Fatalf("pull: %v", got.err)
		}
		if took := time.Since(from); took > bound {
			t.Fatalf("held pull answered %v after the write that queued its message", took)
		}
	case <-time.After(heldFuse()):
		t.Fatalf("held pull not answered within %s", heldFuse())
	}
}

// A pong to a pulling peer ends its held question: the ping arrives by push,
// the receiver queues the pong, and b's question is answered at once rather
// than at its 30-second wait (docs/peers.md, "The held question").
func TestPongEndsHeldQuestionOfPullingPeer(t *testing.T) {
	ctx := context.Background()
	f := newPongHoldFixture(t)
	answer := f.pullHeld(t)

	ping := store.PeerMessage{ID: "0123456789abcdef0123456789abcdef", Kind: store.PeerPing, Body: json.RawMessage("{}")}
	pushed := time.Now()
	if err := f.bCaller.Push(ctx, f.toA, ping); err != nil {
		t.Fatal(err)
	}
	awaitAnswer(t, answer, pushed)
}

// Any other message put in a's outbox for the pulling peer ends the held
// question too: here a's own ping, sent with PeerSend, which the store queues
// the same way as a pong, mail or an answer.
func TestOutboxWriteEndsHeldQuestionOfPullingPeer(t *testing.T) {
	ctx := context.Background()
	f := newPongHoldFixture(t)
	answer := f.pullHeld(t)

	sent := time.Now()
	if _, err := f.a.api.PeerSend(ctx, "b", store.PeerPing, nil, "jo"); err != nil {
		t.Fatal(err)
	}
	awaitAnswer(t, answer, sent)
}
