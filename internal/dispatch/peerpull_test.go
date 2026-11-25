package dispatch_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/testload"
)

// pullingYard starts b's daemon as run does, but with the backoff given: a
// question that fails is not asked again for that long, so a test that waits
// for the next question measures the backoff, not the poll.
func pullingYard(t *testing.T, y *peerYard, backoff time.Duration) {
	t.Helper()
	y.d.PeerHold, y.d.PeerPoll, y.d.Backoff = time.Second, 50*time.Millisecond, backoff
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = y.d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("the daemon did not stop")
		}
	})
}

// within waits for ok, for at most d as the machine is loaded (testload.Scale).
// It is a bound on the behaviour under test, not a fuse against a hang.
func within(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	bound := testload.Scale(d)
	for end := time.Now().Add(bound); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%s: not within %s", what, bound)
		}
	}
}

// pullOf links a and b so that b, which has a's URL and no URL of its own,
// asks a for what waits for b. a holds b's question once b's daemon runs.
func pullOf(t *testing.T, a, b *peerYard) {
	t.Helper()
	link(t, a, b, false, true)
}

// A held question that fails in transport (here, its connection closed with
// no answer) is asked again at once, on a new connection, and not after the
// backoff: a pull that waits for the wait to end would ask nothing for a
// minute.
func TestPeerPullRetriesAHeldQuestionCutShort(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	pullOf(t, a, b)
	pullingYard(t, b, time.Minute)
	until(t, "b's question is held at a", func() bool { return a.holds.Load() >= 1 })

	from := a.asked.Load()
	a.dropNext.Store(true)
	// The dropped question and its retry are two requests, both within the
	// retry's jitter of each other.
	within(t, 5*time.Second, "b asks again after its question was cut short", func() bool {
		return a.asked.Load() >= from+2
	})
}

// A question that got an answer (here 502, from a proxy with nothing behind
// it) is not retried at once: it goes to the backoff as it did.
func TestPeerPullAnAnswerKeepsItsBackoff(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	pullOf(t, a, b)
	pullingYard(t, b, time.Minute)
	until(t, "b's question is held at a", func() bool { return a.holds.Load() >= 1 })

	a.failPulls.Store(true)
	from := a.asked.Load()
	within(t, 5*time.Second, "b's next question is refused", func() bool { return a.asked.Load() > from })
	untilQuiet(t, a)
	// Quiet for the stretch untilQuiet waits for: no retry was sent for the
	// answer, and the backoff of a minute holds the next question back.
	if n := a.asked.Load() - from; n != 1 {
		t.Fatalf("after a refused question b sent %d more before its backoff, want none", n-1)
	}
}

// racingOutbox is the store of a daemon whose pass is made to meet a ping
// in the window it leaves open. A pass reads the outbox twice: kickPullers
// reads it for the pings that want their pong asked for now, and push reads
// it again for what to post. Armed, the first read answers from before the
// write and queue writes the ping between the two reads, so the pass posts a
// ping it did not see when it kicked.
type racingOutbox struct {
	store.API
	armed atomic.Bool
	queue func() string
	id    atomic.Pointer[string]
}

func (r *racingOutbox) PeerOutbox(ctx context.Context, peer string, limit int) ([]store.PeerOut, error) {
	if !r.armed.CompareAndSwap(true, false) {
		return r.API.PeerOutbox(ctx, peer, limit)
	}
	out, err := r.API.PeerOutbox(ctx, peer, limit)
	id := r.queue()
	r.id.Store(&id)
	return out, err
}

// A ping queued for a peer whose questions are failing gets its pong at once:
// the ping ends that peer's backoff and a question is held for the pong, not
// asked after the backoff of a minute (which is what the pong waited for
// before). The ping is queued in the window of a pass, every time, so the
// test does not depend on where the scheduler put the write.
func TestPeerPingEndsAPullersBackoff(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	// Set before b's daemon starts: its goroutines read d.Store from then on.
	race := &racingOutbox{API: b.api}
	race.queue = func() string {
		m, err := b.api.PeerSend(context.Background(), "a", store.PeerPing, nil, "jo")
		if err != nil {
			t.Errorf("queueing b's ping: %v", err)
			return ""
		}
		return m.ID
	}
	b.d.Store = race
	pullOf(t, a, b)
	pullingYard(t, b, time.Minute)
	until(t, "b's question is held at a", func() bool { return a.holds.Load() >= 1 })

	a.failPulls.Store(true)
	from := a.asked.Load()
	within(t, 5*time.Second, "b's next question is refused", func() bool { return a.asked.Load() > from })
	untilQuiet(t, a)
	a.failPulls.Store(false)

	race.armed.Store(true)
	var id *string
	within(t, 5*time.Second, "a pass of b's posts the ping queued in its window", func() bool { id = race.id.Load(); return id != nil })
	start := time.Now()
	within(t, 5*time.Second, "the pong to b's ping, held for it at a", func() bool { return b.pong(t, *id) != nil })
	if took := time.Since(start); took >= 5*time.Second {
		t.Fatalf("the pong came after %s, want well under 5s", took)
	}
}

// A pause (the wall clock moved on, the monotonic one did not: the host
// slept) asks every peer at once, instead of after the backoff it was in.
func TestPeerPauseAsksAtOnce(t *testing.T) {
	t.Parallel()
	clock := newClock()
	a, b := newPeerYard(t, "a", clock), newPeerYard(t, "b", clock)
	pullOf(t, a, b)
	pullingYard(t, b, time.Minute)
	until(t, "b's question is held at a", func() bool { return a.holds.Load() >= 1 })

	a.failPulls.Store(true)
	from := a.asked.Load()
	within(t, 5*time.Second, "b's next question is refused", func() bool { return a.asked.Load() > from })
	untilQuiet(t, a)
	a.failPulls.Store(false)

	// The daemon passes at the poll's rhythm and sees the wall clock an hour
	// ahead of its last pass, with the monotonic clock where it was.
	from = a.asked.Load()
	clock.jump(time.Hour)
	within(t, 5*time.Second, "b asks a again after the pause", func() bool { return a.asked.Load() > from })
}
