package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// receiptYard opens the store at path with the operator, the user mckean
// and a peer b whose key is pinned, linked to send mail. A yard reopened
// on the same path keeps what was written; this sets nothing up twice.
func receiptYard(t *testing.T, path string, fresh bool) *Store {
	t.Helper()
	ctx := context.Background()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		return s
	}
	if err := s.SetSetting(ctx, OperatorSetting, "jo", "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddUser(ctx, "mckean", "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPeer(ctx, Peer{Name: "b", Key: Public(newKey(t))}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AllowPeer(ctx, "b", PeerAllow{Send: []string{PeerMail}}, "jo"); err != nil {
		t.Fatal(err)
	}
	return s
}

// receiptKeys is how many receipts the store holds.
func receiptKeys(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.read(func(tx *bolt.Tx) error {
		n = len(keys(tx, bMailReceipts, ""))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestMailPeerReceiptIsWrittenWithTheMail pins what a receipt says: the
// event it names is the mail's own, the wire id is the message in the
// outbox, and the peer, its key and the participants are as sent.
func TestMailPeerReceiptIsWrittenWithTheMail(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)

	r, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if r.Seq == 0 || r.ID == "" || r.Direction != ReceiptOut || r.Peer != "b" || r.From != "mckean" || r.To != "mckean" {
		t.Fatalf("receipt = %+v", r)
	}
	if r.PeerKey == "" {
		t.Fatalf("receipt has no peer key: %+v", r)
	}
	out, err := s.PeerOutbox(ctx, "b", 0)
	if err != nil || len(out) != 1 || out[0].ID != r.ID {
		t.Fatalf("outbox = %+v, %v; want the receipt's message %s", out, err, r.ID)
	}
	evs, err := s.Events(ctx, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.Seq == r.Seq {
			found = e.Kind == "mail" && e.Data["id"] == r.ID
		}
	}
	if !found {
		t.Fatalf("event %d is not the receipt's mail", r.Seq)
	}
	got, err := s.MailReceiptOf(ctx, r.Seq, "mckean")
	if err != nil || got != r {
		t.Fatalf("MailReceiptOf = %+v, %v; want %+v", got, err, r)
	}
	if _, err := s.MailReceiptOf(ctx, r.Seq, "someone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another name reads the receipt: %v", err)
	}
	// The ordinary send is unchanged: it queues the same mail and adds
	// its own receipt.
	if _, err := s.MailPeer(ctx, "mckean", "b", "again", "mckean"); err != nil {
		t.Fatal(err)
	}
	if n := receiptKeys(t, s); n != 2 {
		t.Fatalf("receipts = %d, want one per mail queued (2)", n)
	}
}

// TestMailReceiptSurvivesOutboxRemovalAndRestart holds the receipt to
// what the outbox does not keep: the message is delivered and gone from
// the outbox, and the yard is closed and opened again.
func TestMailReceiptSurvivesOutboxRemovalAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "a.db")
	s := receiptYard(t, path, true)
	r, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.PeerOutbox(ctx, "b", 0)
	if err != nil || len(out) != 1 {
		t.Fatalf("outbox = %+v, %v", out, err)
	}
	if err := s.PeerDelivered(ctx, "b", out[0].Seq); err != nil {
		t.Fatal(err)
	}
	if out, _ := s.PeerOutbox(ctx, "b", 0); len(out) != 0 {
		t.Fatalf("outbox after delivery = %+v", out)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}

	s = receiptYard(t, path, false)
	defer s.Close(ctx)
	got, err := s.MailReceiptOf(ctx, r.Seq, "mckean")
	if err != nil || got != r {
		t.Fatalf("after restart MailReceiptOf = %+v, %v; want %+v", got, err, r)
	}
}

// TestMailPeerReceiptNotWrittenWhenNotQueued holds the write whole: a
// link that does not carry mail out refuses the mail, and no receipt
// is left for a mail that was never queued.
func TestMailPeerReceiptNotWrittenWhenNotQueued(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	if _, err := s.AllowPeer(ctx, "b", PeerAllow{NoSend: []string{PeerMail}}, "jo"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MailPeerReceipt(ctx, "mckean", "b", "hello", "mckean"); err == nil {
		t.Fatal("mail on a link that does not carry mail out was queued")
	}
	if n := receiptKeys(t, s); n != 0 {
		t.Fatalf("receipts = %d after a refused mail, want 0", n)
	}
	// The refused write rolled back as one: no message was queued either,
	// so a receipt and its outbox message never come apart.
	out, err := s.PeerOutbox(ctx, "b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("outbox = %d messages after a refused mail, want 0", len(out))
	}
}

// failingAfter is an API whose MailPeerReceipt takes the first ok mails
// and then fails, as a link that goes down in the middle of a text.
type failingAfter struct {
	API
	ok int
}

func (f *failingAfter) MailPeerReceipt(ctx context.Context, to, peer, text, actor string, files ...BlobFile) (MailReceipt, error) {
	if f.ok == 0 {
		return MailReceipt{}, errors.New("link down")
	}
	f.ok--
	return f.API.MailPeerReceipt(ctx, to, peer, text, actor, files...)
}

// TestMailPeerReceiptsPrefixFailure holds a text sent in parts to what
// was taken: the receipts of the parts before the failed one come back
// with the error, and the store has exactly those.
func TestMailPeerReceiptsPrefixFailure(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	text := strings.Repeat("x", 3*MaxMailBytes)
	if n := len(SplitMail(text)); n < 3 {
		t.Fatalf("text splits into %d parts, want at least 3", n)
	}

	got, err := MailPeerReceipts(&failingAfter{API: s, ok: 1}, ctx, "mckean", "b", text, "mckean")
	if err == nil || !strings.Contains(err.Error(), "1 of") {
		t.Fatalf("err = %v, want the prefix of one part named", err)
	}
	if len(got) != 1 {
		t.Fatalf("receipts = %+v, want the one part taken", got)
	}
	if n := receiptKeys(t, s); n != 1 {
		t.Fatalf("store holds %d receipts, want the 1 taken", n)
	}
	if r, err := s.MailReceiptOf(ctx, got[0].Seq, "mckean"); err != nil || r != got[0] {
		t.Fatalf("taken part's receipt = %+v, %v", r, err)
	}
}

// TestMailPeerReceiptsWholeText holds a text that sends in full to a
// receipt per part, in order.
func TestMailPeerReceiptsWholeText(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	text := strings.Repeat("y", 2*MaxMailBytes)
	want := len(SplitMail(text))

	got, err := MailPeerReceipts(s, ctx, "mckean", "b", text, "mckean")
	if err != nil || len(got) != want {
		t.Fatalf("receipts = %d, %v; want %d", len(got), err, want)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Seq <= got[i-1].Seq || got[i].ID == got[i-1].ID {
			t.Fatalf("parts out of order or repeated: %+v then %+v", got[i-1], got[i])
		}
	}
	if n := receiptKeys(t, s); n != want {
		t.Fatalf("store holds %d receipts, want %d", n, want)
	}
}

// mailEvents is how many mail events the log holds.
func mailEvents(t *testing.T, s *Store) int {
	t.Helper()
	evs, err := s.EventsOfKind(context.Background(), "mail", 0)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

// TestMailLocalReceiptIsWrittenWithTheMail pins a local receipt: it names
// the mail's own event, its direction is local, it has no peer, and the
// sender reads it back, as with a peer mail's.
func TestMailLocalReceiptIsWrittenWithTheMail(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)

	r, err := s.MailLocalReceipt(ctx, "mckean", "hello", "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if r.Seq == 0 || r.Direction != ReceiptLocal || r.Peer != "" || r.ID != "" || r.From != "mckean" || r.To != "mckean" {
		t.Fatalf("receipt = %+v", r)
	}
	evs, err := s.EventsOfKind(ctx, "mail", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Seq != r.Seq {
		t.Fatalf("mail events = %+v, want the receipt's %d", evs, r.Seq)
	}
	got, err := s.MailReceiptOf(ctx, r.Seq, "mckean")
	if err != nil || got != r {
		t.Fatalf("MailReceiptOf = %+v, %v; want %+v", got, err, r)
	}
	// An answer is a local mail too, under its own receipt.
	ra, err := s.MailReplyReceipt(ctx, "mckean", "re: hello", "mckean", r.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if ra.Seq == r.Seq || ra.Direction != ReceiptLocal {
		t.Fatalf("answer's receipt = %+v", ra)
	}
	if n := receiptKeys(t, s); n != 2 {
		t.Fatalf("receipts = %d, want one per mail (2)", n)
	}
}

// TestMailTextReceiptsWholeText holds a local text sent in parts to a
// receipt per part, in order, and the answer's parts to the same.
func TestMailTextReceiptsWholeText(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)
	text := strings.Repeat("z", 2*MaxMailBytes)
	want := len(SplitMail(text))
	got, err := MailTextReceipts(s, ctx, "mckean", text, "mckean")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != want {
		t.Fatalf("receipts = %d, want one per part (%d)", len(got), want)
	}
	for i, r := range got {
		if r.Direction != ReceiptLocal || (i > 0 && r.Seq <= got[i-1].Seq) {
			t.Fatalf("receipt %d = %+v, not local and in order", i, r)
		}
	}
	first := got[0].Seq
	rep, err := MailReplyTextReceipts(s, ctx, "mckean", text, "mckean", first)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep) != want {
		t.Fatalf("answer's receipts = %d, want %d", len(rep), want)
	}
	if n := receiptKeys(t, s); n != 2*want {
		t.Fatalf("receipts = %d, want %d", n, 2*want)
	}
}

// TestMailReceiptRollsBackWithTheMail fails the receipt's write, the last
// write of a mail, after its event and its outbox message are in the same
// transaction. The mail must not be written at all: no event, no message
// and no receipt, for local mail and for mail to a peer.
func TestMailReceiptRollsBackWithTheMail(t *testing.T) {
	ctx := context.Background()
	s := receiptYard(t, filepath.Join(t.TempDir(), "a.db"), true)
	defer s.Close(ctx)

	failing := writeReceipt
	writeReceipt = func(*bolt.Tx, MailReceipt) error { return errors.New("receipt write refused") }
	defer func() { writeReceipt = failing }()

	before := mailEvents(t, s)
	if _, err := s.MailLocalReceipt(ctx, "mckean", "local", "mckean"); err == nil {
		t.Fatal("local mail was written with its receipt refused")
	}
	if _, err := s.MailPeerReceipt(ctx, "mckean", "b", "peer", "mckean"); err == nil {
		t.Fatal("peer mail was queued with its receipt refused")
	}
	if n := mailEvents(t, s); n != before {
		t.Fatalf("mail events = %d after the refused writes, want %d", n, before)
	}
	if n := receiptKeys(t, s); n != 0 {
		t.Fatalf("receipts = %d after the refused writes, want 0", n)
	}
	out, err := s.PeerOutbox(ctx, "b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("outbox = %d messages after the refused write, want 0", len(out))
	}
}
