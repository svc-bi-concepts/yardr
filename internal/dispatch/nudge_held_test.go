package dispatch_test

import (
	"context"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// yardr-n8wn: the yardmaster sat idle with mail unread from 05:23 to 05:45
// on 11-24, and no nudge, nudge_held or harness event was logged for it
// (delivery yard, seq 10114-10118). A pane that stays unreadable after the
// Ctrl-L redraw holds the nudge with nothing typed and nothing logged: that
// hold said nothing, so the silence could not be told from a nudge that
// was never due. An idle crew member with mail ends in a typed nudge or in
// a logged hold, and the hold is logged once per wait, not once per pass.
func TestUnreadablePaneHoldsNudgeAndSaysSoOnce(t *testing.T) {
	t.Parallel()
	s, f, d := crewSetup(t, store.CrewMember{})
	ctx := context.Background()
	r := &redrawCrewRunner{script: []read{{"", false}}, crewRunner: f}
	d.Runners["fake"] = r
	must(t, d.Reconcile(ctx))
	member, err := s.GetCrew(ctx, "yardmaster")
	must(t, err)

	must(t, s.Mail(ctx, "yardmaster", "deploy freeze", "chris"))
	for range 4 {
		must(t, d.Reconcile(ctx))
	}
	if len(r.nudges) != 0 || r.submits != 0 {
		t.Fatalf("typed into an unreadable pane: nudges %q, Enter %d", r.nudges, r.submits)
	}
	if n, _ := kindCount(t, s, member.Journal, "nudge_held"); n != 1 {
		t.Fatalf("nudge_held events = %d, want one for the wait", n)
	}
	if _, ev := kindCount(t, s, member.Journal, "nudge_held"); ev.Data["reason"] == nil {
		t.Errorf("nudge_held carries no reason: %v", ev.Data)
	}

	// The pane reads again: the mail is nudged, and the wait is over.
	r.script = []read{{"", true}}
	must(t, d.Reconcile(ctx))
	if len(r.nudges) != 1 {
		t.Fatalf("nudges after the pane reads again = %q, want one", r.nudges)
	}
}
