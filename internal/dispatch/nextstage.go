package dispatch

import (
	"time"

	"yardr.dev/yardr/internal/store"
)

// NextStage is one stage a flow allows the bead to move to, with who would
// pick it up there, so a brief can say when to choose it.
type NextStage struct {
	Stage    string
	Terminal bool   // entering it closes the bead
	Group    string // the group routed for the stage; "" when none is
	Runner   string // that group's runner
	Peer     string // the linked yard that works the stage, for a peer group
	// Else are the further groups of the stage's route, in order: the
	// bead is not there yet, so none is chosen, and the quota when it gets
	// there is not known. They start agents, as Group then does.
	Else []string
	// Overflow is the linked yard a full group passes the bead to, when the
	// route names a peer group after its groups, with how long the bead
	// waits in the stage first (OverflowAfter); "" when none does.
	Overflow      string
	OverflowAfter time.Duration
}

// NextOutcome is one outcome the bead's stage offers and where it leads.
type NextOutcome struct {
	Outcome string
	// NextStage is where reporting the outcome takes this bead now, with who
	// picks it up there: the stage a limit sends it to once its rounds are
	// used, not the flow's own edge.
	NextStage
	// Limit is the outcome as the bead's rounds stand for it, for an outcome
	// that has a limit: which round reporting it would be, or that its rounds
	// are used. Nil for one that has none.
	Limit *store.OutcomeNow
}
