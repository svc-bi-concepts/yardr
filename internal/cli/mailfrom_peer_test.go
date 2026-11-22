package cli

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

type mailPeerChange struct {
	store.API
	before func(int) error
	parts  int
}

func (s *mailPeerChange) MailFromPeer(ctx context.Context, to, text, from, peerKey string, files ...store.BlobFile) error {
	s.parts++
	if err := s.before(s.parts); err != nil {
		return err
	}
	return s.API.MailFromPeer(ctx, to, text, from, peerKey, files...)
}

func TestMailFromHomeFollowsPeerIdentity(t *testing.T) {
	t.Parallel()
	for _, at := range []int{1, 2} {
		for _, unlink := range []bool{false, true} {
			t.Run(map[bool]string{false: "rename", true: "unlink"}[unlink]+"/part"+strconv.Itoa(at), func(t *testing.T) {
				ctx := context.Background()
				north, south, east := newCLIYard(t, "north"), newCLIYard(t, "south"), newCLIYard(t, "east")
				cliEnv(t, north, south, east)
				south.must(t, "add", "north", strings.TrimSpace(north.must(t, "invite")))
				south.must(t, "add", "east", strings.TrimSpace(east.must(t, "invite")))
				if _, err := north.st.PutCrew(ctx, store.CrewMember{Name: "dispatcher", Runner: "herdr", Dir: t.TempDir()}, "jo"); err != nil {
					t.Fatal(err)
				}
				if _, err := south.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Dir: t.TempDir()}, "jo"); err != nil {
					t.Fatal(err)
				}
				south.must(t, "allow", "north", "--send", "mail", "--expose", "yardmaster")
				south.must(t, "allow", "east", "--send", "mail", "--expose", "yardmaster")
				if _, err := run(t, south.a.crewCmd(), "set", "yardmaster", "peers=north,east"); err != nil {
					t.Fatal(err)
				}
				st := &mailPeerChange{API: south.st, before: func(part int) error {
					if part != at {
						return nil
					}
					if unlink {
						if err := south.st.DeletePeer(ctx, "north", "jo"); err != nil {
							return err
						}
					} else if _, err := south.st.RenamePeer(ctx, "north", "archive", "jo"); err != nil {
						return err
					}
					_, err := south.st.RenamePeer(ctx, "east", "north", "jo")
					return err
				}}
				send := &app{st: st, env: testEnv(t, north.home)}
				_, err := run(t, send.rootCmd(), "--home", south.home, "--actor", "dispatcher", "mail", "yardmaster", strings.Repeat("x", 5000))
				want := 2
				if unlink {
					want = at - 1
					if err == nil || !strings.Contains(err.Error(), "no link to the sending yard") {
						t.Fatalf("mail after unlink = %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				page, err := south.st.Inbox(ctx, "yardmaster", 10)
				if err != nil {
					t.Fatal(err)
				}
				var mail []store.Event
				for _, e := range page.Events {
					if e.Data["from"] == "dispatcher" {
						mail = append(mail, e)
					}
				}
				if len(mail) != want {
					t.Fatalf("recorded %d parts, want %d", len(mail), want)
				}
				for _, e := range mail {
					if unlink {
						if gone, _ := e.Data["from_peer_unlinked"].(bool); !gone {
							t.Fatalf("earlier part acquired a new sender: %+v", e)
						}
					} else if e.Actor != "dispatcher@archive" || e.Data["from_peer"] != "archive" {
						t.Fatalf("mail acquired the wrong yard: %+v", e)
					}
				}
				if !unlink {
					south.a.actor = "yardmaster"
					seq := strconv.FormatUint(mail[0].Seq, 10)
					if _, err := run(t, south.a.replyCmd(), seq, "Friday.", "--crew", "yardmaster"); err != nil {
						t.Fatal(err)
					}
					if box, err := south.st.PeerOutbox(ctx, "archive", 0); err != nil || len(box) != 1 {
						t.Fatalf("original yard's replies = %+v, %v", box, err)
					}
				}
				if box, err := south.st.PeerOutbox(ctx, "north", 0); err != nil || len(box) != 0 {
					t.Fatalf("unrelated yard's replies = %+v, %v", box, err)
				}
			})
		}
	}
}
