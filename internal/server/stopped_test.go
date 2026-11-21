package server

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A STOPPED file put in the home of a running server ends it on the next
// tick, the normal way: the socket and the pidfiles go, and yard.db is
// closed and not written after that. The server is the writer that a yard
// stopped by hand has to lose (yard-999p).
func TestServerStopsWhenItsYardIsStopped(t *testing.T) {
	c := home(t)
	logged := &lockedBuffer{}
	done := start(t, &runner{c: c, rev: "r", homeCheck: 20 * time.Millisecond,
		log: slog.New(slog.NewTextHandler(logged, nil))})
	before, err := os.Stat(c.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.StoppedFile(), []byte("who and when\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the server ended with %v, want a plain stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the server kept running on a stopped home")
	}
	if !strings.Contains(logged.String(), "STOPPED file; stopping") {
		t.Fatalf("the log does not say why the server stopped:\n%s", logged.String())
	}
	for _, p := range []string{c.Socket(), c.PIDFile(), c.PIDStart()} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind by the stop: %v", filepath.Base(p), err)
		}
	}
	after, err := os.Stat(c.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatalf("yard.db changed after the stop: mtime %v -> %v, size %d -> %d",
			before.ModTime(), after.ModTime(), before.Size(), after.Size())
	}
	// Nothing writes after the server is gone: a second look, a tick later,
	// finds the file as the stop left it.
	time.Sleep(100 * time.Millisecond)
	again, err := os.Stat(c.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(after.ModTime()) || again.Size() != after.Size() {
		t.Fatalf("yard.db written after the server stopped: mtime %v -> %v", after.ModTime(), again.ModTime())
	}
}

// A server does not open a stopped home's store at all: a server that would
// stop on its first tick must not write yard.db on the way.
func TestServerRefusesAStoppedHome(t *testing.T) {
	c := home(t)
	if err := os.WriteFile(c.StoppedFile(), []byte("who and when\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &runner{c: c, rev: "r", log: quiet}
	_, replacing, err := r.serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "is stopped") || !strings.Contains(err.Error(), c.StoppedFile()) {
		t.Fatalf("serve on a stopped home: %v, want a refusal naming the STOPPED file", err)
	}
	if replacing {
		t.Fatal("a refused serve asked to replace")
	}
	if _, err := os.Stat(c.DBPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("yard.db was opened on a stopped home: %v", err)
	}
}
