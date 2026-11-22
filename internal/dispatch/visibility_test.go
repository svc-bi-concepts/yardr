package dispatch_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yardr.dev/yardr/internal/dispatch"
	"yardr.dev/yardr/internal/scripttest"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// visibilityYard is a daemon whose GitHub reader answers from answers, by
// the depot's directory, and counts the calls it is asked.
type visibilityYard struct {
	t       *testing.T
	s       store.API
	d       *dispatch.Dispatcher
	now     time.Time
	mu      sync.Mutex
	asked   map[string]int // calls by directory
	answers map[string]string
	errs    map[string]error
}

func newVisibilityYard(t *testing.T) *visibilityYard {
	t.Helper()
	home := t.TempDir()
	y := &visibilityYard{
		t:       t,
		s:       storetest.NewIn(t, home),
		now:     time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC),
		asked:   map[string]int{},
		answers: map[string]string{},
		errs:    map[string]error{},
	}
	y.d = &dispatch.Dispatcher{
		Store:   y.s,
		Home:    home,
		Log:     slog.New(slog.NewTextHandler(&lockedBuf{}, nil)),
		Runners: map[string]dispatch.Runner{},
		HostVisibility: func(_ context.Context, dir string) (string, error) {
			y.mu.Lock()
			defer y.mu.Unlock()
			y.asked[dir]++
			if err := y.errs[dir]; err != nil {
				return "", err
			}
			return y.answers[dir], nil
		},
	}
	dispatch.SetClock(y.d, func() time.Time { return y.now })
	// The yardmaster is the crew member the event is delivered to: its
	// inbox is read back by name.
	if _, err := y.s.PutCrew(context.Background(), store.CrewMember{Name: "yardmaster", Runner: "fake"}, "me"); err != nil {
		t.Fatal(err)
	}
	return y
}

// check runs the daily check at the yard's time now.
func (y *visibilityYard) check() { dispatch.CheckVisibility(context.Background(), y.d) }

// mismatches are the depot_visibility events the yardmaster's inbox holds.
func (y *visibilityYard) mismatches() []store.Event {
	y.t.Helper()
	page, err := y.s.Inbox(context.Background(), "yardmaster", 0)
	if err != nil {
		y.t.Fatal(err)
	}
	var out []store.Event
	for _, e := range page.Events {
		if e.Kind == store.DepotVisibilityEvent {
			out = append(out, e)
		}
	}
	return out
}

// lockedBuf is the log a daemon writes to, safe for the goroutines that log.
type lockedBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

// The daily check asks GitHub once a depot a day: a depot that differs from
// GitHub's is told to the yardmaster once, a depot that matches is not told,
// a dir depot is not asked, and the depot's recorded visibility is unchanged.
func TestVisibilityCheckTellsTheYardmasterOfAMismatch(t *testing.T) {
	t.Parallel()
	y := newVisibilityYard(t)
	ctx := context.Background()
	web, lib, docs := t.TempDir(), t.TempDir(), t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(y.s.PutDepot(ctx, store.Depot{Name: "web", Path: web, Base: "main"}, "operator"))
	must(y.s.PutDepot(ctx, store.Depot{Name: "lib", Path: lib, Base: "main", Visibility: store.DepotPrivate}, "operator"))
	must(y.s.PutDepot(ctx, store.Depot{Name: "docs", Path: docs, Kind: store.DepotDir}, "operator"))
	y.answers[web] = store.DepotPublic  // recorded private: a mismatch
	y.answers[lib] = store.DepotPrivate // recorded private: a match
	y.answers[docs] = store.DepotPublic // never asked

	y.check()
	if y.asked[web] != 1 || y.asked[lib] != 1 || y.asked[docs] != 0 {
		t.Fatalf("GitHub asked %v; want web and lib once, docs never", y.asked)
	}
	got := y.mismatches()
	if len(got) != 1 {
		t.Fatalf("the yardmaster's inbox holds %d mismatches, want 1: %+v", len(got), got)
	}
	if e := got[0]; e.Data["depot"] != "web" || e.Data["recorded"] != store.DepotPrivate || e.Data["actual"] != store.DepotPublic {
		t.Fatalf("the mismatch names %+v, want web, recorded private, actual public", e.Data)
	}
	if d, _ := y.s.GetDepot(ctx, "web"); d.Visibility != store.DepotPrivate {
		t.Fatalf("the check changed web to %s; it must never change a depot", d.Visibility)
	}

	// The same day, a second pass asks no one again.
	y.now = y.now.Add(3 * time.Hour)
	y.check()
	if y.asked[web] != 1 || y.asked[lib] != 1 {
		t.Fatalf("a second pass the same day asked GitHub again: %v", y.asked)
	}
	if n := len(y.mismatches()); n != 1 {
		t.Fatalf("a second pass the same day told %d mismatches, want still 1", n)
	}

	// The next UTC day, the check runs again, and tells the mismatch again.
	y.now = time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC)
	y.check()
	if y.asked[web] != 2 || y.asked[lib] != 2 {
		t.Fatalf("the next day asked %v, want web and lib a second time", y.asked)
	}
	if n := len(y.mismatches()); n != 2 {
		t.Fatalf("the next day's check told %d mismatches in all, want 2", n)
	}
}

// A depot GitHub does not answer for is logged and not flagged, and it is
// asked again on the next day, not on every pass of this one.
func TestVisibilityCheckSkipsWhatGitHubDoesNotAnswer(t *testing.T) {
	t.Parallel()
	y := newVisibilityYard(t)
	ctx := context.Background()
	noRemote := t.TempDir()
	if err := y.s.PutDepot(ctx, store.Depot{Name: "local", Path: noRemote, Base: "main"}, "operator"); err != nil {
		t.Fatal(err)
	}
	y.errs[noRemote] = fmt.Errorf("gh repo view: exit status 1")

	y.check()
	y.check()
	if y.asked[noRemote] != 1 {
		t.Fatalf("a depot GitHub does not answer for was asked %d times in one day, want 1", y.asked[noRemote])
	}
	if n := len(y.mismatches()); n != 0 {
		t.Fatalf("an unanswered depot was flagged: %d events", n)
	}
	y.now = y.now.Add(24 * time.Hour)
	y.check()
	if y.asked[noRemote] != 2 {
		t.Fatalf("the next day asked %d times in all, want 2", y.asked[noRemote])
	}
}

// A host that hangs on every depot ends the pass at the check's deadline,
// leaves the depots due (a later pass asks them again) and flags none.
func TestVisibilityCheckStopsAtItsDeadline(t *testing.T) {
	t.Parallel()
	y := newVisibilityYard(t)
	ctx := context.Background()
	var calls int
	var mu sync.Mutex
	y.d.HostVisibility = func(ctx context.Context, _ string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		<-ctx.Done()
		return "", ctx.Err()
	}
	for _, n := range []string{"a", "b", "c"} {
		if err := y.s.PutDepot(ctx, store.Depot{Name: n, Path: t.TempDir(), Base: "main"}, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	dispatch.SetVisibilityDeadline(y.d, 100*time.Millisecond)
	start := time.Now()
	y.check()
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the check took %v against a 100ms deadline", took)
	}
	mu.Lock()
	first := calls
	mu.Unlock()
	if first != 1 {
		t.Fatalf("a hanging host was asked %d times before the deadline, want 1", first)
	}
	if n := len(y.mismatches()); n != 0 {
		t.Fatalf("a hanging host was flagged: %d events", n)
	}
	y.check()
	mu.Lock()
	second := calls
	mu.Unlock()
	if second != 2 {
		t.Fatalf("the depots were not left due: %d calls after two passes, want 2", second)
	}
}

// With no reader, the daemon asks no one: a test that runs passes never
// reaches GitHub.
func TestVisibilityCheckIsOffWithoutAReader(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	s := storetest.NewIn(t, home)
	ctx := context.Background()
	if err := s.PutDepot(ctx, store.Depot{Name: "web", Path: t.TempDir(), Base: "main"}, "operator"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "fake"}, "me"); err != nil {
		t.Fatal(err)
	}
	d := &dispatch.Dispatcher{Store: s, Home: home, Log: slog.New(slog.NewTextHandler(&lockedBuf{}, nil)), Runners: map[string]dispatch.Runner{}}
	dispatch.CheckVisibility(ctx, d)
	page, err := s.Inbox(ctx, "yardmaster", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Events {
		if e.Kind == store.DepotVisibilityEvent {
			t.Fatalf("the check with no reader told %+v", e.Data)
		}
	}
}

// ReadGitHubVisibility reads gh's answer: public is public, private and
// internal are private, and anything else, or a gh that fails, is an error
// the check logs and does not flag. The stand-in gh stands in for the real
// one, which no test reaches.
func TestReadHostVisibilityReadsGh(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := t.TempDir()
	for answer, want := range map[string]string{"PUBLIC\n": store.DepotPublic, "PRIVATE\n": store.DepotPrivate, "INTERNAL\n": store.DepotPrivate} {
		scriptBody := fmt.Sprintf("#!/bin/sh\nprintf '%%s' '%s'\n", strings.TrimSpace(answer))
		scripttest.Write(t, filepath.Join(bin, "gh"), scriptBody)
		got, err := dispatch.ReadHostVisibility(context.Background(), repo)
		if err != nil || got != want {
			t.Fatalf("gh says %q: %q, %v; want %q", answer, got, err, want)
		}
	}
	scripttest.Write(t, filepath.Join(bin, "gh"), "#!/bin/sh\nprintf '%s' SECRET\n")
	if got, err := dispatch.ReadHostVisibility(context.Background(), repo); err == nil {
		t.Fatalf("an unknown visibility read as %q", got)
	}
	scripttest.Write(t, filepath.Join(bin, "gh"), "#!/bin/sh\necho 'no git remote' >&2\nexit 1\n")
	if _, err := dispatch.ReadHostVisibility(context.Background(), repo); err == nil {
		t.Fatal("a failing gh read as an answer")
	}
}
