package dispatch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// visibilityTimeout bounds one gh call, so a host that does not answer
// holds the pass up for no longer than this.
const visibilityTimeout = 30 * time.Second

// visibility is the daily check of each git depot's recorded visibility
// against the host's record of its repository: once a UTC day, one gh call per depot. A
// depot that differs is told to the yardmaster (store.TellDepotVisibility),
// and the check never changes the value: only the operator does. A dir depot
// is skipped, as is a depot the host does not answer for (no remote it knows, gh
// not installed): that is logged, not flagged. Nothing runs unless the
// daemon was given a reader (HostVisibility), which the tests leave out so
// that no test reaches the host.
//
// A day gets one attempt, as telemetry's does, so a depot the host does not
// answer is not asked again until the next day. The day is kept in memory:
// a daemon that restarts makes one more pass over the depots that day.
func (d *Dispatcher) visibility(ctx context.Context) {
	if d.HostVisibility == nil {
		return
	}
	day := d.now().UTC().Format(time.DateOnly)
	if day == d.visibilityDay {
		return
	}
	d.visibilityDay = day
	depots, err := d.Store.Depots(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.Log.Warn("visibility: reading the depots", "err", err)
		}
		d.visibilityDay = "" // the next pass tries again
		return
	}
	for _, r := range depots {
		if ctx.Err() != nil {
			d.visibilityDay = "" // the daemon is stopping: the next one checks
			return
		}
		if r.IsDir() || r.Path == "" {
			continue
		}
		actual, err := d.HostVisibility(ctx, r.Path)
		if err != nil {
			d.Log.Info("visibility: the host does not say", "depot", r.Name, "err", err)
			continue
		}
		if actual == r.Visibility {
			continue
		}
		if err := d.Store.TellDepotVisibility(ctx, r.Name, r.Visibility, actual); err != nil {
			if ctx.Err() == nil {
				d.Log.Warn("visibility: telling the yardmaster", "depot", r.Name, "err", err)
			}
			continue
		}
		d.Log.Info("visibility: depot differs from its host's record", "depot", r.Name,
			"recorded", r.Visibility, "actual", actual)
	}
}

// ReadHostVisibility is the visibility the host records for the repository
// dir is a checkout of, as gh says it (gh repo view, in dir, which names the repository
// by its remote): public, or private for a private or an internal repository,
// which is private to the yard.
func ReadHostVisibility(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, visibilityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "visibility", "-q", ".visibility")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh repo view: %w", err)
	}
	switch v := strings.ToUpper(strings.TrimSpace(string(out))); v {
	case "PUBLIC":
		return store.DepotPublic, nil
	case "PRIVATE", "INTERNAL":
		return store.DepotPrivate, nil
	default:
		return "", fmt.Errorf("gh repo view: visibility %q is not one it knows", v)
	}
}
