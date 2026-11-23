package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/storetest"
)

// stoppedHome is a yard with a store and a STOPPED file in its home, as a
// person leaves one to keep the yard stopped. It returns the home, and the
// store file's modification time and size before any command runs.
func stoppedHome(t *testing.T) (home string, mtime int64, size int64) {
	t.Helper()
	home = newHomeDir(t)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	storetest.NewIn(t, home)
	if err := os.WriteFile(filepath.Join(home, "STOPPED"), []byte("who and when\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	return home, fi.ModTime().UnixNano(), fi.Size()
}

// noConnect replaces connect for the test: a command on a stopped home that
// reaches it has started or dialled a server, which is what the test refuses.
// It counts the calls, so a test can say how many there were.
func noConnect(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := connect
	connect = func(context.Context, *config.Config) (store.API, error) {
		*calls++
		return nil, errors.New("test: connect called on a stopped home")
	}
	t.Cleanup(func() { connect = old })
	return calls
}

// A stopped home starts no server for a read or a write, and yard.db is not
// written: the command is refused with a line that names the STOPPED file.
func TestStoppedHomeRefusesWithoutStartingAServer(t *testing.T) {
	home, mtime, size := stoppedHome(t)
	calls := noConnect(t)

	for _, args := range [][]string{
		{"bead", "list"},
		{"dep", "add", "a", "b"},
	} {
		a := &app{env: testEnv(t, home)}
		_, err := run(t, a.rootCmd(), args...)
		if err == nil {
			t.Fatalf("yardr %s on a stopped home: no error", strings.Join(args, " "))
		}
		msg := err.Error()
		if !strings.Contains(msg, "is stopped") || !strings.Contains(msg, filepath.Join(home, "STOPPED")) || !strings.Contains(msg, "Remove that file by hand") {
			t.Errorf("yardr %s: %q does not say the home is stopped, naming its STOPPED file and how to start it", strings.Join(args, " "), msg)
		}
	}

	if *calls != 0 {
		t.Errorf("connect was called %d times on a stopped home, want none: a server would have started", *calls)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != mtime || fi.Size() != size {
		t.Errorf("yard.db changed on a stopped home: mtime %v -> %v, size %d -> %d",
			mtime, fi.ModTime(), size, fi.Size())
	}
}

// yardr up is refused on a stopped home, as every command is: a STOPPED file
// is for a yard that is retired or moved, and up does not bring it back. The
// refusal comes before any connect, so no server starts and yard.db stays as
// it is; removing the file by hand is the one way back.
func TestUpOnAStoppedHomeIsRefused(t *testing.T) {
	home, mtime, size := stoppedHome(t)
	calls := noConnect(t)

	a := &app{env: testEnv(t, home)}
	out, err := run(t, a.rootCmd(), "up")
	if err == nil || !strings.Contains(err.Error(), "is stopped") || !strings.Contains(err.Error(), "Remove that file by hand") {
		t.Fatalf("up on a stopped home: %v\n%s", err, out)
	}
	if *calls != 0 {
		t.Errorf("connect called %d times by up, want none", *calls)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != mtime || fi.Size() != size {
		t.Errorf("yard.db changed on a stopped home")
	}
	if _, err := os.Stat(filepath.Join(home, "STOPPED")); err != nil {
		t.Errorf("up removed the STOPPED file: %v", err)
	}
}

// The daemon of a stopped yard with no server starts nothing and exits 0, as
// the server does on its stop: a login service that restarts a failing daemon
// would otherwise loop on it. No connect is made and yard.db stays as it is.
func TestDaemonOnAStoppedHomeExitsCleanly(t *testing.T) {
	home, mtime, size := stoppedHome(t)
	calls := noConnect(t)

	a := &app{env: testEnv(t, home)}
	if _, err := run(t, a.rootCmd(), "daemon"); err != nil {
		t.Fatalf("daemon on a stopped home: %v, want a clean exit", err)
	}
	if *calls != 0 {
		t.Errorf("connect called %d times by the daemon, want none", *calls)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != mtime || fi.Size() != size {
		t.Errorf("yard.db changed on a stopped home")
	}
}

// A home with no STOPPED file is as before: the store connects, and no
// refusal is made.
func TestStoreUnstoppedHomeConnects(t *testing.T) {
	home := newHomeDir(t)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	storetest.NewIn(t, home)
	calls := 0
	old := connect
	connect = func(context.Context, *config.Config) (store.API, error) {
		calls++
		return storetest.NewIn(t, t.TempDir()), nil
	}
	t.Cleanup(func() { connect = old })

	a := &app{env: testEnv(t, home)}
	if _, err := a.store(context.Background()); err != nil {
		t.Fatalf("store on an unstopped home: %v", err)
	}
	if calls != 1 {
		t.Errorf("connect called %d times, want 1", calls)
	}
}

// status starts no server, and on a stopped home it does not tell the
// operator that a command would start one.
func TestStatusOnAStoppedHomeSaysNoCommandStartsIt(t *testing.T) {
	t.Parallel()
	home, _, _ := stoppedHome(t)
	out, _ := executeOut(&app{env: testEnv(t, home)}, "status")
	if !strings.Contains(out, "the home has a STOPPED file, so no command starts it") {
		t.Errorf("status on a stopped home:\n%s", out)
	}
	if strings.Contains(out, "any yardr command that needs the yard starts it") {
		t.Errorf("status on a stopped home promises a start:\n%s", out)
	}
}

// yardr server start is no way round the refusal: on a stopped home it starts
// no server either.
func TestServerStartOnAStoppedHomeIsRefused(t *testing.T) {
	home, mtime, size := stoppedHome(t)
	calls := noConnect(t)
	a := &app{env: testEnv(t, home)}
	_, err := run(t, a.rootCmd(), "server", "start")
	if err == nil || !strings.Contains(err.Error(), "is stopped") {
		t.Fatalf("server start on a stopped home: %v", err)
	}
	if *calls != 0 {
		t.Errorf("connect called %d times, want none", *calls)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != mtime || fi.Size() != size {
		t.Errorf("yard.db changed")
	}
}

// yardr down on a stopped home goes without the store: no server starts,
// yard.db stays as it is, and the daemon, the other parts and the server are
// reported, with no agents.
func TestDownOnAStoppedHomeStartsNoServer(t *testing.T) {
	home, mtime, size := stoppedHome(t)
	calls := noConnect(t)
	a := &app{env: testEnv(t, home)}
	out, err := run(t, a.rootCmd(), "down", "--json")
	if err != nil {
		t.Fatalf("down on a stopped home: %v\n%s", err, out)
	}
	var rep struct {
		Down   bool
		Agents []any
		Parts  []struct{ Part string }
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !rep.Down || len(rep.Agents) != 0 {
		t.Errorf("down report: %s", out)
	}
	parts := map[string]bool{}
	for _, p := range rep.Parts {
		parts[p.Part] = true
	}
	if !parts["daemon"] || !parts["server"] {
		t.Errorf("parts of the report lack the daemon or the server: %s", out)
	}
	if *calls != 0 {
		t.Errorf("connect called %d times, want none", *calls)
	}
	fi, err := os.Stat(filepath.Join(home, "yard.db"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().UnixNano() != mtime || fi.Size() != size {
		t.Errorf("yard.db changed")
	}
}
