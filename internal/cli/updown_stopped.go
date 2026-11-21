package cli

import (
	"context"
	"fmt"
	"slices"

	"yardr.dev/yardr/internal/config"
)

// stopOthers stops the long-running parts after the daemon, each that runs,
// and reports each; settings say which of them up would start again.
func stopOthers(cfg *config.Config, settings map[string]string, yardr string) []yardPart {
	var stopped []yardPart
	for _, k := range longRunnings[1:] {
		p := yardPart{Part: k.Name}
		run, err := k.stop(cfg, stopWait)
		switch {
		case err != nil:
			p.Running, p.Error, p.Do = true, err.Error(), fmt.Sprintf("%s %s stop", yardr, k.Name)
		case len(run) > 0:
			p.Stopped, p.PID = true, run[0].PID
			if partAddr(cfg, settings, k) == "" {
				// up starts a part by the setting or the service file
				// that names its address.
				p.Detail = fmt.Sprintf("yardr up does not start it again: %s %s start", yardr, k.Name)
			}
		}
		stopped = append(stopped, p)
	}
	return stopped
}

// downStopped is down on a home with a STOPPED file and no server: the store
// is not asked, so no server starts and yard.db stays as it is. Without the
// store there are no agents to stop and no settings to read; the daemon and
// the other parts still stop, as do the login services if asked.
func (a *app) downStopped(ctx context.Context, cfg *config.Config, login loginScope) (*downReport, error) {
	rep := &downReport{Home: cfg.Home, Agents: []yardAgent{}}
	yardr := yardrAt(cfg.Home)
	herdr := yardPart{Part: partHerdr, Detail: "left running: other yards and your own panes are in it"}
	if herdrBin, _ := herdrProgram(ctx); herdrBin == "" {
		herdr.Detail = "not found on this machine"
	} else if herdr.Running = herdrServer(ctx, herdrBin).State == herdrUp; !herdr.Running {
		herdr.Detail = "not running"
	}
	daemon := yardPart{Part: svcDaemon, Running: config.DaemonRunning(cfg.DaemonLock()), PID: config.DaemonPID(cfg.DaemonLock())}
	pid, err := stopDaemon(cfg, stopWait)
	if err != nil {
		daemon.Error, daemon.Do = err.Error(), fmt.Sprintf("%s daemon stop --wait 2m, then: %s down", yardr, yardr)
		rep.Parts = append(append([]yardPart{daemon}, others(cfg)...), yardPart{Part: partServer}, herdr)
		return rep, nil
	}
	daemon.Running, daemon.Stopped, daemon.PID = false, pid != 0, pid
	settings := map[string]string{}
	stopped := stopOthers(cfg, settings, yardr)
	if login != loginKept {
		rep.Login = downLogin(ctx, cfg, settings, login == loginAll)
	}
	rep.Parts = append(append([]yardPart{daemon}, stopped...), yardPart{Part: partServer}, herdr)
	rep.Down = !daemon.Running && !slices.ContainsFunc(stopped, func(p yardPart) bool { return p.Error != "" })
	return rep, nil
}
