package cli

import (
	"fmt"
	"io"

	"yardr.dev/yardr/internal/store"
)

// printPeerParties lists whom the linked yards expose to this one, under
// the users: the address mail to each goes to, whether they are a person
// or a crew member there, whether mail reaches them, and the line their
// yard gives of what they do. Nothing for none.
func printPeerParties(w io.Writer, parties []store.PeerParty) {
	if len(parties) == 0 {
		return
	}
	rows := [][]string{}
	for _, e := range parties {
		is := "crew"
		if e.Kind == store.RosterUser {
			is = "person"
		}
		rows = append(rows, []string{e.Address(), is, takesMail(e), or(e.Role, "-")})
	}
	fmt.Fprintln(w)
	table(w, "OF A LINKED YARD\tIS\tMAIL\tROLE", rows)
}

// takesMail is whether mail from this yard reaches e, and whose grant is
// missing where it does not: both sides of a link allow mail.
func takesMail(e store.PeerParty) string {
	switch {
	case !e.Sends:
		return fmt.Sprintf("no (yardr peer allow %s --send mail)", e.Peer)
	case !e.Mail:
		return fmt.Sprintf("no (%s takes none from this yard)", e.Peer)
	}
	return "yes"
}
