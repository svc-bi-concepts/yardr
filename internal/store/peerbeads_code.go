package store

import "fmt"

// CodeLine is how yardr bead show says what code crossed with bead, "" when
// none did: for one that came from a yard, where its work starts; for one
// sent out, whose branch its branch is, or why the branch that came back is
// not here.
func (p BeadPeer) CodeLine(bead Bead) string {
	g := p.Git
	switch {
	case g == nil:
		return ""
	case p.Direction != "out":
		return fmt.Sprintf("its work starts from %s at %s (%s), as %s said", or(g.Base, "the commit"), short(g.BaseCommit), g.Remote, p.Yard())
	case p.FetchedAt != nil:
		return fmt.Sprintf("branch %s came from %s: its %s at %s, fetched %s from %s. This yard has not tested it",
			BranchOfBead(bead), p.Yard(), g.Branch, short(g.Commit), stamp(*p.FetchedAt), g.Remote)
	case p.NotTaken != "":
		return fmt.Sprintf("the branch %s returned (%s at %s) was not taken: %s", p.Yard(), g.Branch, short(g.Commit), p.NotTaken)
	}
	line := fmt.Sprintf("the branch %s returned (%s at %s) is not fetched yet, and its outcome %s waits for it", p.Yard(), g.Branch, short(g.Commit), p.Outcome)
	switch {
	case p.FetchGaveUp != nil:
		line += fmt.Sprintf(": given up after %s (%s); yardr bead unhold %s tries again", tries(p.FetchTries), p.FetchError, bead.ID)
	case p.FetchTries > 0 && p.FetchRetryAt != nil:
		line += fmt.Sprintf(": %s so far, the next at %s (%s)", tries(p.FetchTries), p.FetchRetryAt.Local().Format("15:04:05"), p.FetchError)
	}
	return line
}
