package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"yardr.dev/yardr/internal/store"
)

func TestMailFromPeerRejectsLocalAgents(t *testing.T) {
	t.Parallel()
	for _, who := range []string{"crew", "starting bead", "running bead", "starting exec", "running exec"} {
		t.Run(who, func(t *testing.T) {
			api, own := replyYard(t)
			ctx := context.Background()
			linkNorth(t, api)
			var caller context.Context
			if who == "crew" {
				if _, err := api.PutCrew(ctx, store.CrewMember{Name: "yardmaster", Runner: "herdr"}, "ada"); err != nil {
					t.Fatal(err)
				}
				caller = crewPane(t, own, ctx, "yardmaster")
			} else {
				runner := "herdr"
				var cfg map[string]any
				if strings.HasSuffix(who, "exec") {
					runner = "exec"
					cfg = map[string]any{"cmd": "true"}
				}
				if err := api.PutGroup(ctx, store.Group{Name: "workers", Runner: runner, Config: cfg}, "ada"); err != nil {
					t.Fatal(err)
				}
				if _, err := api.CreateBead(ctx, store.NewBead{ID: "yd-1", Title: "the shed", Stage: "new"}, "ada"); err != nil {
					t.Fatal(err)
				}
				sess, err := api.Claim(ctx, "yd-1", "workers", "new", "ada")
				if err != nil {
					t.Fatal(err)
				}
				caller = store.WithCaller(ctx, os.Getpid())
				if strings.HasPrefix(who, "starting") {
					caller = store.WithSession(caller, sess.ID)
				} else if err := api.MarkRunning(ctx, sess.ID, map[string]any{"shell_pid": os.Getpid(), "pid": os.Getpid()}); err != nil {
					t.Fatal(err)
				}
			}
			if err := own.MailFromPeer(caller, "ada", "forged", "dispatcher", "north"); err == nil {
				t.Fatal("a local agent wrote mail as dispatcher@north")
			}
			box, err := own.UserInbox(ctx, "ada", 0, "ada")
			if err != nil || len(box.Mail) != 0 {
				t.Fatalf("inbox after refused mail = %+v, %v", box, err)
			}
			if err := own.MailFromPeer(ctx, "ada", "from another home", "dispatcher", "north"); err != nil {
				t.Fatalf("mail from an unrecognized local caller: %v", err)
			}
		})
	}
}
