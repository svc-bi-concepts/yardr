package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"yardr.dev/yardr/internal/store"
)

// The operator's inbox lists each open card that holds a delivery on one line,
// with the command that answers it; a card that holds none is not printed.
func TestPrintHeldCards(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	printHeldCards(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("no cards printed %q", buf.String())
	}
	printHeldCards(&buf, []store.HeldCard{{Card: "yd-k3f9", Source: "talk", Sender: "U999", Channel: "C1", Held: 2}})
	want := "Held: 1 card(s) hold messages from senders no user maps yet\n" +
		"  yd-k3f9  2 held from talk sender U999 in channel C1  answer: yardr bead show yd-k3f9\n"
	if buf.String() != want {
		t.Fatalf("held cards print\n%q\nwant\n%q", buf.String(), want)
	}
}

// A crew member's inbox shows an identity_held notice as one line: the card,
// the source and sender, the count and the answer command, never a message.
func TestPrintHeldNotice(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 19, 9, 30, 0, 0, time.UTC)
	e := store.Event{Seq: 42, At: at, Kind: store.KindIdentityHeld, Data: map[string]any{
		"card": "yd-k3f9", "source": "talk", "sender": "U999", "channel": "C1", "held": float64(2),
	}}
	var buf bytes.Buffer
	printHeldNotice(&buf, e)
	line := buf.String()
	for _, want := range []string{"42 ", "2 message(s) held from talk sender U999 in channel C1, on card yd-k3f9",
		"answer: yardr bead show yd-k3f9"} {
		if !strings.Contains(line, want) {
			t.Fatalf("notice %q lacks %q", line, want)
		}
	}
}
