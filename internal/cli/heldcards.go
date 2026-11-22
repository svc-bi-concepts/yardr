package cli

import (
	"fmt"
	"io"

	"yardr.dev/yardr/internal/store"
)

// printHeldCards lists the open mapping cards that hold deliveries, one line
// each, for the operator's inbox: where the held messages came from, how many
// wait, and the command that answers the card. The messages themselves stay on
// the card, so nothing of them is printed here. Nothing for none.
func printHeldCards(w io.Writer, cards []store.HeldCard) {
	if len(cards) == 0 {
		return
	}
	fmt.Fprintf(w, "Held: %d card(s) hold messages from senders no user maps yet\n", len(cards))
	for _, c := range cards {
		channel := ""
		if c.Channel != "" {
			channel = " in channel " + store.Letters(c.Channel)
		}
		fmt.Fprintf(w, "  %s  %d held from %s sender %s%s  answer: yardr bead show %s\n",
			c.Card, c.Held, store.Letters(c.Source), store.Letters(c.Sender), channel, c.Card)
	}
}

// printHeldNotice shows an identity_held event in a crew member's inbox: the
// card its held messages wait on, the source and sender they came from, and how
// many wait. The message text is not in the event; the card holds it.
func printHeldNotice(w io.Writer, e store.Event) {
	card, _ := e.Data["card"].(string)
	source, _ := e.Data["source"].(string)
	sender, _ := e.Data["sender"].(string)
	channel, _ := e.Data["channel"].(string)
	if channel != "" {
		channel = " in channel " + store.Letters(channel)
	}
	fmt.Fprintf(w, "%6d %s  %v message(s) held from %s sender %s%s, on card %s: answer: yardr bead show %s\n",
		e.Seq, e.At.Local().Format("01-02 15:04"), e.Data["held"], store.Letters(source), store.Letters(sender), channel, card, card)
}
