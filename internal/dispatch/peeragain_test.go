package dispatch_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
)

// A bead sent to a yard again (docs/peers.md, "A bead sent again"): where
// its work starts the second time, and what the yard that works it knows of
// the first.

// sendingAgain is the flow sending with a way back from review into the
// stage the peer works, which is how a bead is sent a second time.
const sendingAgain = "new -> build\nbuild -> review on done | decide on question | decide on failed | decide on refused | decide on undelivered\nreview -> merged | build\ndecide -> build\nmerged terminal\n"

// sendsAgain gives y's depot web the flow sendingAgain.
func (y *peerYard) sendsAgain(t *testing.T) {
	t.Helper()
	putFlow(t, y, "again", sendingAgain)
	must(t, y.api.SetFlowPointer(context.Background(), "web", "", "again", "jo"))
}

// again moves y's bead back into the stage its peer works, which sends it
// once more on the next pass.
func (y *peerYard) again(t *testing.T, id string) {
	t.Helper()
	_, err := y.api.Advance(context.Background(), id, "build", store.Expect{}, "jo")
	must(t, err)
}

// A bead that came back with a branch and is sent again starts, at the
// yard that works it, at the commit that yard returned: what it returns
// the second time comes after this yard's branch and is taken by
// fast-forward. The yard that works it names its earlier bead.
func TestPeerBeadSentAgain(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	link(t, a, b, true, true)
	remote := sharedRemote(t)
	bRepo := b.clones(t, remote)
	b.takes(t, "a", "web")
	aRepo := a.sendsFrom(t, remote, "b", nil)
	a.sendsAgain(t)

	mine := a.bead(t, "Add the export")
	a.pass(t)
	first := b.from(t, "a")
	if first.Peer.WasHere() != "" || first.Peer.RefusedBefore() != "" || len(first.Peer.Before) != 0 {
		t.Fatalf("b's first bead names earlier ones: %+v", first.Peer.Before)
	}
	one := b.builds(t, bRepo, first)
	b.done(t, first.ID)
	b.pass(t)
	a.pass(t)
	if got := a.settled(t, mine.ID, "review"); branchAt(t, aRepo, store.Branch(mine.ID)) != one {
		t.Fatalf("a's bead after the first return = %+v, branch at %s", got, branchAt(t, aRepo, store.Branch(mine.ID)))
	}

	a.again(t, mine.ID)
	a.pass(t)
	sent := a.beadEvents(t, mine.ID, "peer_bead_sent")
	if len(sent) != 2 || sent[1].Data["base_commit"] != one || sent[1].Data["base"] != store.Branch(first.ID) {
		t.Fatalf("a's second peer_bead_sent = %+v, want it to start at %s", sent, one)
	}
	second := b.from(t, "a")
	fam, err := b.api.Family(context.Background(), second.ID)
	must(t, err)
	if second.ID == first.ID || second.Peer.Git == nil || second.Peer.Git.BaseCommit != one || fam.Base != one {
		t.Fatalf("b's second bead starts from %+v, family %+v, want %s", second.Peer.Git, fam, one)
	}
	if b := second.Peer.Before; len(b) != 1 || b[0].Bead != first.ID || b[0].Outcome != "done" || b[0].ReturnedAt == nil || b[0].ClosedBy != "" {
		t.Fatalf("b's second bead names before it %+v", b)
	}
	if line := second.Peer.WasHere(); !strings.HasPrefix(line, "a's "+mine.ID+" was here before as "+first.ID+" (returned done ") || second.Peer.RefusedBefore() != "" {
		t.Fatalf("b's second bead says %q, %q", line, second.Peer.RefusedBefore())
	}
	// Listed, a bead does not carry them: they are read with the bead.
	if bs, err := b.api.ListBeads(context.Background(), store.BeadFilter{Label: "from-a"}); err != nil || len(bs) != 1 || bs[0].Peer == nil || bs[0].Peer.Before != nil {
		t.Fatalf("b's listed bead = %+v, %v", bs, err)
	}
	two := b.builds(t, bRepo, second)
	if gitIn(t, bRepo, "rev-parse", two+"^") != one {
		t.Fatal("b's second work does not start at what it returned first")
	}
	b.done(t, second.ID)
	b.pass(t)
	a.pass(t)
	got := a.settled(t, mine.ID, "review")
	if got.Hold || branchAt(t, aRepo, store.Branch(mine.ID)) != two {
		t.Fatalf("a's bead after the second return = %+v, %+v, branch at %s, want %s", got, got.Away(), branchAt(t, aRepo, store.Branch(mine.ID)), two)
	}
	if f := a.beadEvents(t, mine.ID, "peer_bead_fetched"); len(f) != 2 || f[1].Data["commit"] != two || len(a.beadEvents(t, mine.ID, "peer_bead_not_fetched")) != 0 {
		t.Fatalf("a's peer_bead_fetched = %+v", f)
	}
}

// With commits of this yard's own on the bead's branch since the other yard
// returned it, the branch is pushed to the remote the other yard fetches
// from, and the bead is sent again from it: that yard starts where this
// yard's branch stands. While the push fails the bead is not sent, and each
// failure is an event on the bead; once the push works, the bead goes.
func TestPeerBeadSentAgainWithTheBranchMovedOn(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	link(t, a, b, true, true)
	remote := sharedRemote(t)
	bRepo := b.clones(t, remote)
	b.takes(t, "a", "web")
	aRepo := a.sendsFrom(t, remote, "b", nil)
	a.sendsAgain(t)

	mine := a.bead(t, "Add the export")
	a.pass(t)
	first := b.from(t, "a")
	b.builds(t, bRepo, first)
	b.done(t, first.ID)
	b.pass(t)
	a.pass(t)
	a.settled(t, mine.ID, "review")
	branch := store.Branch(mine.ID)
	gitIn(t, aRepo, "checkout", "-q", branch)
	own := commitIn(t, aRepo, "review.txt", "a's own\n")
	gitIn(t, aRepo, "checkout", "-q", "main")

	// The remote is not there while the first push is made: it fails.
	origin := gitIn(t, aRepo, "remote", "get-url", "origin")
	gitIn(t, aRepo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	a.again(t, mine.ID)
	queued := len(a.events(t, "peer_message_queued"))
	a.pass(t)
	if failed := a.beadEvents(t, mine.ID, "peer_bead_push_failed"); len(failed) == 0 || failed[0].Actor != "yard" || failed[0].Data["branch"] != branch {
		t.Fatalf("a's push failure = %+v", failed)
	}
	if len(a.events(t, "peer_message_queued")) != queued || len(a.beadEvents(t, mine.ID, "peer_bead_sent")) != 1 {
		t.Fatal("a sent a bead whose branch did not push")
	}

	gitIn(t, aRepo, "remote", "set-url", "origin", origin)
	// A start's failure is read on one pass and its retry claimed on a later
	// one, once the daemon's backoff has passed on the test's clock.
	for i := 0; i < 10 && len(a.beadEvents(t, mine.ID, "peer_bead_sent")) < 2; i++ {
		clock.now = clock.now.Add(time.Minute)
		a.pass(t)
	}
	b.pass(t)
	sent := a.beadEvents(t, mine.ID, "peer_bead_sent")
	if len(sent) != 2 || sent[1].Data["base_commit"] != own || sent[1].Data["base"] != branch {
		t.Fatalf("a's second peer_bead_sent = %+v, want it to start at %s", sent, own)
	}
	if at := gitIn(t, remote, "rev-parse", "--verify", "refs/heads/"+branch); at != own {
		t.Fatalf("the remote has %s at %s, want %s", branch, at, own)
	}
	if second := b.from(t, "a"); second.Peer.Git == nil || second.Peer.Git.BaseCommit != own || second.Peer.Git.Base != branch {
		t.Fatalf("b's second bead starts from %+v, want %s", second.Peer.Git, own)
	}
	if at := branchAt(t, aRepo, branch); at != own {
		t.Fatalf("a's branch was moved to %s", at)
	}
}

// A branch that holds nothing its base does not is no reason to refuse: the
// bead is sent as a first sending is, from the base.
func TestPeerBeadSentAgainFromTheBase(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	link(t, a, b, true, true)
	remote := sharedRemote(t)
	bRepo := b.clones(t, remote)
	b.takes(t, "a", "web")
	aRepo := a.sendsFrom(t, remote, "b", nil)
	a.sendsAgain(t)

	mine := a.bead(t, "Add the export")
	a.pass(t)
	first := b.from(t, "a")
	b.builds(t, bRepo, first)
	b.done(t, first.ID)
	b.pass(t)
	a.pass(t)
	a.settled(t, mine.ID, "review")
	// The base moved on, and the bead's branch was made anew from it.
	base := commitIn(t, aRepo, "b.txt", "two\n")
	gitIn(t, aRepo, "push", "-q", "origin", "main")
	gitIn(t, aRepo, "branch", "-f", store.Branch(mine.ID), base)

	a.again(t, mine.ID)
	a.pass(t)
	sent := a.beadEvents(t, mine.ID, "peer_bead_sent")
	if len(sent) != 2 || sent[1].Data["base_commit"] != base || sent[1].Data["base"] != "main" {
		t.Fatalf("a's second peer_bead_sent = %+v, want the base %s", sent, base)
	}
}

// A bead someone of the working yard closed went back as refused. When it
// comes again, the new bead says so, with who closed the earlier one and
// why. Sent under another handle it says nothing: the handle is all the
// match goes by.
func TestPeerBeadRefusedBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	link(t, a, b, true, true)
	a.sends(t, "b", nil)
	b.takes(t, "a", "web")

	mine := a.bead(t, "Add the export")
	a.pass(t)
	first := b.from(t, "a")
	_, err := b.api.CloseBead(ctx, first.ID, "not ours\nto build", store.Expect{}, "jo")
	must(t, err)
	b.pass(t)
	a.pass(t)
	if got := a.get(t, mine.ID); got.Stage != "decide" {
		t.Fatalf("a's bead after b closed its own = %+v", got)
	}
	a.again(t, mine.ID)
	a.pass(t)
	second := b.from(t, "a")
	bf := second.Peer.Before
	if second.ID == first.ID || len(bf) != 1 || bf[0].Bead != first.ID || bf[0].ClosedBy != "jo" || bf[0].Reason != "not ours to build" ||
		bf[0].Outcome != store.OutcomeRefused || bf[0].ReturnedAt == nil || second.Peer.WasHere() != "" {
		t.Fatalf("b's second bead names before it %+v, %q", bf, second.Peer.WasHere())
	}
	want := "this yard refused it before (" + first.ID + ", " + bf[0].ReturnedAt.Local().Format("01-02") + ", closed by jo: not ours to build)"
	if got := second.Peer.RefusedBefore(); got != want {
		t.Fatalf("b's second bead says %q, want %q", got, want)
	}
	// The earlier bead names nothing: what came after it is not before it.
	if e := b.get(t, first.ID); len(e.Peer.Before) != 0 {
		t.Fatalf("b's first bead names %+v", e.Peer.Before)
	}
	// The same text as a new bead of a's, so under another handle.
	other := a.bead(t, "Add the export")
	a.pass(t)
	bs, err := b.api.ListBeads(ctx, store.BeadFilter{Label: "from-a"})
	must(t, err)
	for _, l := range bs {
		if got := b.get(t, l.ID); got.Peer.Bead == other.ID && (len(got.Peer.Before) != 0 || got.Peer.RefusedBefore() != "") {
			t.Fatalf("a bead under another handle says %q", got.Peer.RefusedBefore())
		}
	}
	if len(bs) != 2 {
		t.Fatalf("b has %d open beads from a, want two", len(bs))
	}
}
