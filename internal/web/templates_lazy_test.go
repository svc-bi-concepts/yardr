package web

import (
	"strings"
	"testing"

	"yardr.dev/yardr/internal/storetest"
)

// A server makes its partials only when it first renders one: a page that
// is served never streams, and a stream that is never opened renders no
// partial. Parsing them at start cost each server a clone of the shell.
func TestPartialsParseWhenUsed(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	srv, err := NewServer(st, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if srv.PartialsParsed() {
		t.Fatal("a new server made its partials, want none until one renders")
	}
	if srv.page("talk") == nil {
		t.Fatal(`page "talk" is missing`)
	}
	if srv.PartialsParsed() {
		t.Fatal("serving a page made the partials, want them only when one renders")
	}
	var b strings.Builder
	if err := srv.renderPartial(&b, "csrf", nil); err != nil {
		t.Fatal(err)
	}
	if !srv.PartialsParsed() {
		t.Fatal("rendering a partial did not make the partials")
	}
}

// A server parses only the pages it serves. Parsing every page at start
// cost each fixture several megabytes under -race (yardr-hzxe), so this
// holds the lazy parse: a new server holds no page set, a served page is
// parsed once and kept, and a partial or an unknown name is no page.
func TestPageSetsParseWhenServed(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	srv, err := NewServer(st, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := srv.PagesParsed(); n != 0 {
		t.Fatalf("a new server parsed %d page sets, want none", n)
	}
	first := srv.page("talk")
	if first == nil {
		t.Fatal(`page "talk" is missing`)
	}
	if again := srv.page("talk"); again != first {
		t.Error(`page "talk" was parsed twice`)
	}
	if n := srv.PagesParsed(); n != 1 {
		t.Errorf("after serving one page: %d page sets, want 1", n)
	}
	for _, name := range []string{"shell", "card", "no-such-page"} {
		if srv.page(name) != nil {
			t.Errorf("page(%q) is a page set, want none", name)
		}
	}
	if n := srv.PagesParsed(); n != 1 {
		t.Errorf("after the partials and an unknown name: %d page sets, want 1", n)
	}
}
