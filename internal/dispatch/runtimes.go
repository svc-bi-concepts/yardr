package dispatch

import (
	"context"
	"time"

	"yardr.dev/yardr/internal/store"
)

// runtimeDown notes that g's runtime could not start anything. The first
// failure of an outage is logged as runtime_down for the group, so the
// yardmaster hears of it once; later ones only lengthen the wait, doubling up
// to RuntimeBackoffMax.
func (d *Dispatcher) runtimeDown(ctx context.Context, g store.Group, err error) {
	o := d.down[g.Name]
	if o == nil {
		o = &outage{delay: d.RuntimeBackoff}
		d.down[g.Name] = o
		d.Log.Warn("runtime down; the group's beads wait", "group", g.Name, "runner", g.Runner, "err", err)
		if lerr := d.Store.LogDispatch(ctx, "", "runtime_down", actor, map[string]any{
			"group": g.Name, "runner": g.Runner, "reason": err.Error()}); lerr != nil {
			d.Log.Warn("runtime_down event", "err", lerr)
		}
	} else {
		o.delay = min(2*o.delay, d.RuntimeBackoffMax)
	}
	o.until = time.Now().Add(o.delay)
}

// runtimeUp ends g's outage, if it had one, once a Start got past the
// runtime.
func (d *Dispatcher) runtimeUp(ctx context.Context, g store.Group) {
	if d.down[g.Name] == nil {
		return
	}
	delete(d.down, g.Name)
	d.Log.Info("runtime back", "group", g.Name, "runner", g.Runner)
	_ = d.Store.LogDispatch(ctx, "", "runtime_up", actor, map[string]any{"group": g.Name, "runner": g.Runner})
}
