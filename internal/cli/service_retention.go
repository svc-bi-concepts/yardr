package cli

import (
	"slices"
	"strings"

	"yardr.dev/yardr/internal/config"
)

// daemonKeep are the retention settings the daemon prunes by. Its service
// writes them off, so that a reinstall never brings the prune back: the
// yard keeps its events, webhook bodies and inbox log unless an operator
// turns retention on. The installing environment's value wins (keepOff).
var daemonKeep = []string{"EVENT_RETENTION", "HOOK_PAYLOAD_RETENTION", "INBOX_RETENTION"}

// keepOff is env with each of the named yardr variables set to off, unless
// env already sets it.
func keepOff(env []string, names []string) []string {
	for _, name := range names {
		key := config.Key(name)
		if !slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, key+"=") }) {
			env = append(env, key+"=off")
		}
	}
	return env
}
