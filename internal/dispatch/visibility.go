package dispatch

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"yardr.dev/yardr/internal/store"
)

// visibilityTimeout bounds one gh call, and visibilityTotal the whole check
// across depots, so a host that does not answer holds the pass up for no
// longer than the total.
const (
	visibilityTimeout = 15 * time.Second
	visibilityTotal   = 60 * time.Second
)

// visibility is the daily check of each git depot's recorded visibility
// against the host's record of its repository: once a UTC day, one gh call per depot. A
// depot that differs is told to the yardmaster (store.TellDepotVisibility),
// and the check never changes the value: only the operator does. A dir depot
// is skipped, as is a depot the host does not answer for (no remote it knows, gh
// not installed): that is logged, not flagged. Nothing runs unless the
// daemon was given a reader (HostVisibility), which the tests leave out so
// that no test reaches the host.
//
// A day gets one attempt per depot, as telemetry's does, so a depot the host
// does not answer is not asked again until the next day. The whole check has
// one deadline: the depots not reached by then stay due and are asked on the
// next pass. The day is kept in memory: a daemon that restarts makes one more
// pass over the depots that day.
func (d *Dispatcher) visibility(ctx context.Context) {
	if d.HostVisibility == nil {
		return
	}
	day := d.now().UTC().Format(time.DateOnly)
	if day != d.visibilityDay {
		d.visibilityDay = day
		d.visibilityDone = map[string]bool{}
	}
	depots, err := d.Store.Depots(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.Log.Warn("visibility: reading the depots", "err", err)
		}
		return
	}
	total := d.visibilityDeadline
	if total == 0 {
		total = visibilityTotal
	}
	dctx, cancel := context.WithTimeout(ctx, total)
	defer cancel()
	for i, r := range depots {
		if r.IsDir() || r.Path == "" || d.visibilityDone[r.Name] {
			continue
		}
		if ctx.Err() != nil {
			return // the daemon is stopping: the next one checks
		}
		if dctx.Err() != nil {
			d.visibilityStopped(depots[i:])
			return
		}
		actual, err := d.HostVisibility(dctx, r.Path)
		if dctx.Err() != nil {
			// The deadline, or the daemon stopping, cut this call short: the
			// depot is neither answered nor attempted.
			if ctx.Err() == nil {
				d.visibilityStopped(depots[i:])
			}
			return
		}
		d.visibilityDone[r.Name] = true
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

// visibilityStopped logs, once, that the check stopped at its deadline, with
// the count of git depots it left for the next pass.
func (d *Dispatcher) visibilityStopped(rest []store.Depot) {
	left := 0
	for _, r := range rest {
		if !r.IsDir() && r.Path != "" && !d.visibilityDone[r.Name] {
			left++
		}
	}
	d.Log.Info("visibility: the check stopped at its deadline", "left", left)
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
