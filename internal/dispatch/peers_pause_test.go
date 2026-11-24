package dispatch

import (
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
)

// A pause is the wall clock moving on by more than the monotonic one, by
// more than peerPauseGap. A pass that comes a poll later, with both clocks
// moving together, is no pause; nor is a first pass with no mark.
func TestPausedSince(t *testing.T) {
	t.Parallel()
	wall := time.Date(2026, 11, 24, 7, 32, 0, 0, time.UTC)
	last := clockMark{wall: wall, mono: 100 * time.Second}
	cases := []struct {
		name string
		last clockMark
		wall time.Time
		mono time.Duration
		want bool
	}{
		{"first pass has no mark", clockMark{}, wall.Add(time.Hour), 0, false},
		{"a poll later, both clocks moved", last, wall.Add(5 * time.Second), 105 * time.Second, false},
		{"the monotonic clock moved with the wall", last, wall.Add(time.Hour), 100*time.Second + time.Hour, false},
		{"the wall clock ran ahead of the monotonic one", last, wall.Add(time.Hour), 105 * time.Second, true},
		{"just under the gap", last, wall.Add(peerPauseGap), 100 * time.Second, false},
		{"just over the gap", last, wall.Add(peerPauseGap + time.Second), 100 * time.Second, true},
	}
	for _, c := range cases {
		if got := pausedSince(c.last, c.wall, c.mono); got != c.want {
			t.Errorf("%s: pausedSince = %v, want %v", c.name, got, c.want)
		}
	}
}

// Only a message that expects an answer ends a backoff: a ping and an ask.
func TestExpectsAnswer(t *testing.T) {
	t.Parallel()
	for kind, want := range map[string]bool{store.PeerPing: true, store.PeerAsk: true, store.PeerMail: false, store.PeerFile: false} {
		if got := expectsAnswer(kind); got != want {
			t.Errorf("expectsAnswer(%q) = %v, want %v", kind, got, want)
		}
	}
}
