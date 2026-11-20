package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

type replyPeerChange struct {
	store.API
	change func() error
}

func (s *replyPeerChange) EventsSince(ctx context.Context, after uint64, limit int) ([]store.Event, error) {
	events, err := s.API.EventsSince(ctx, after, limit)
	if err == nil && s.change != nil {
		change := s.change
		s.change = nil
		err = change()
	}
	return events, err
}

func TestMailReplyFollowsPeerIdentity(t *testing.T) {
	t.Parallel()
	for _, wired := range []bool{false, true} {
		for _, unlink := range []bool{false, true} {
			t.Run(map[bool]string{false: "direct", true: "received"}[wired]+"/"+map[bool]string{false: "renamed", true: "unlinked"}[unlink], func(t *testing.T) {
				ctx := context.Background()
				north, south, east := newCLIYard(t, "north"), newCLIYard(t, "south"), newCLIYard(t, "east")
				cliEnv(t, north, south, east)
				nInvite := strings.TrimSpace(north.must(t, "invite", "--url", north.srv.URL))
				eInvite := strings.TrimSpace(east.must(t, "invite", "--url", east.srv.URL))
				south.must(t, "add", "north", nInvite)
				south.must(t, "add", "east", eInvite)
				if _, err := south.st.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr", Dir: t.TempDir()}, "jo"); err != nil {
					t.Fatal(err)
				}
				south.must(t, "allow", "north", "--send", "mail", "--receive", "mail", "--expose", "yardmaster")
				south.must(t, "allow", "east", "--send", "mail", "--expose", "yardmaster")
				if _, err := run(t, south.a.crewCmd(), "set", "yardmaster", "peers=north,east"); err != nil {
					t.Fatal(err)
				}
				p, err := south.st.GetPeer(ctx, "north")
				if err != nil {
					t.Fatal(err)
				}
				if wired {
					body, _ := json.Marshal(map[string]string{"to": "yardmaster", "from": "dispatcher", "text": "freeze?"})
					got, err := south.own.PeerReceive(ctx, "north", p.Key, []store.PeerMessage{{ID: "original", Kind: store.PeerMail, Body: body}})
					if err != nil || len(got) != 1 || got[0].Refused != "" {
						t.Fatalf("received mail = %+v, %v", got, err)
					}
				} else if err := south.st.MailFromPeer(ctx, "yardmaster", "freeze?", "dispatcher", p.Key); err != nil {
					t.Fatal(err)
				}
				seq := mailSeq(t, south.st)
				original := south.st
				south.a.st = &replyPeerChange{API: original, change: func() error {
					if unlink {
						if err := original.DeletePeer(ctx, "north", "jo"); err != nil {
							return err
						}
					} else if _, err := original.RenamePeer(ctx, "north", "archive", "jo"); err != nil {
						return err
					}
					_, err := original.RenamePeer(ctx, "east", "north", "jo")
					return err
				}}
				south.a.actor = "yardmaster"
				out, err := run(t, south.a.replyCmd(), seq, "Friday.", "--crew", "yardmaster")
				if unlink {
					if err == nil || !strings.Contains(err.Error(), "no longer linked") {
						t.Fatalf("reply after unlink = %q, %v", out, err)
					}
				} else {
					if err != nil || !strings.Contains(out, "dispatcher@archive queued") {
						t.Fatalf("reply after rename = %q, %v", out, err)
					}
					box, err := original.PeerOutbox(ctx, "archive", 0)
					if err != nil || len(box) != 1 {
						t.Fatalf("original yard's outbox = %+v, %v", box, err)
					}
					wantRe := ""
					if wired {
						wantRe = "original"
					}
					if box[0].Re != wantRe {
						t.Fatalf("reply reference = %q, want %q", box[0].Re, wantRe)
					}
				}
				if box, err := original.PeerOutbox(ctx, "north", 0); err != nil || len(box) != 0 {
					t.Fatalf("unrelated yard's outbox = %+v, %v", box, err)
				}
			})
		}
	}
}
