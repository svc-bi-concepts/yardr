package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"yardr.dev/yardr/internal/config"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/internal/yardkey"
)

func (a *app) mailCmd() *cobra.Command {
	var with string
	var after uint64
	var files []string
	cmd := &cobra.Command{
		Use:   "mail <name>[@<peer>] <text|-> [--file <path>]...",
		Short: "Send a message to the inbox of a crew member or a user, or read a conversation back (--with)",
		Long: `Sends a message to the inbox of a crew member or of a user. A text over
one mail's size (4 KB) is sent as several mails, each numbered (1/3); past
16 it is refused: send it as a file (--file).

A user is a person of the yard (yardr user list), the operator among them.
A crew member's session and a user mail a user; an agent working a bead does
not, its channel is the bead. Nothing nudges a person: they read with yardr
inbox.

--file <path> sends a file with the mail, and may be given more than once
(up to ` + strconv.Itoa(store.MaxMailFiles) + `); the text may then be left out. The yard keeps the
file in its home, in blobs/ under the SHA-256 of its bytes, once however often
it is sent, and where the mail is read (yardr inbox, a crew member's nudge,
yardr chat) it stands under the mail's words, on a line of its own starting
with "◆", as "1 image: <path>", "1 page: <path>" or "1 file: <path> (name)":
a crew member reads the file there, by its path. What kind it is is read
from its bytes: an image is a PNG or a JPEG, a page is HTML, anything else
a file, which carries the path's base name. One over the
yard's limit (yardr settings get ` + store.BlobMaxSetting + `, 8 MiB unless set) is refused.

--file with <name>@<peer> sends the file to the linked yard with the mail,
of any kind, when the link carries files out (yardr peer
allow <peer> --send file) and the peer's yardr understands them (yardr peer
show <peer>); otherwise the mail is refused and nothing is sent. One file
may have up to yardr settings get
` + store.PeerFileMaxSetting + ` bytes (the yard's limit for a file unless set). The other
yard keeps it only when its link takes files and it is within that yard's own
limit; where it does not, the mail still arrives, and says "not taken" with
the reason where the file's path would be.

--with <name> sends nothing and prints the mail between you and that crew member
or user, both directions, oldest first: seq, time, whether its receiver has
read it (acknowledged it with yardr inbox --ack <seq>), sender and text. --after
<seq> prints only what is newer; --json prints a list of
{seq, at, from, to, text, read}, with files on mail that carries any. --with <name>@<peer> prints the mail
between you and a crew member or person of a linked yard; what you wrote says queued,
sent or failed (delivery in --json) instead, since the other yard does not
say what was read. It goes by the link, so a peer renamed since (yardr peer
rename) shows its mail from before under the name it has now.

<name>@<peer> is a crew member or a person of a linked yard, <peer> being this
yard's name for the link (yardr peer list); yardr user list has whom each
linked yard exposes to this one. That mail (up to 16 KB) is queued for the peer
and the daemon delivers it, so the command returns before it has arrived:
yardr peer show <peer> lists what still waits. The link must carry mail out
(yardr peer allow <peer> --send mail). That is all a user of the yard needs; a
crew member must be allowed the peer as well (yardr crew set <crew member>
peers=<peer>, the operator's to give), and an agent working a bead mails no
other yard. Mail the other yard does not take comes back to the inbox of the
crew member or user who wrote it, as mail from the yard that says why.

Mail from another yard is shown as from <name>@<peer> and marked "` + store.OutsideMail + `".
Where the operator trusts that name as a user of this yard (yardr peer allow
<peer> --trust <name>@<peer>=<user>) it is marked "(as <user>)" instead.
Answer it with yardr reply <seq>, the number its inbox line starts with.
Mail from a yard that is linked no
more (yardr peer rm) is shown as from "<name> at a yard no longer linked (was
<peer>)", with no address: a yard added under that name later is another yard.`,
		Example: `  yardr mail yardmaster - < freeze.md
  yardr mail mckean - < assembly-red.md
  yardr mail mckean - --file yard.png < painted.md
  yardr mail --with yardmaster
  yardr mail --with yardmaster --after 412 --json
  yardr mail yardmaster@north - < go-version.md
  yardr mail yardmaster@north - --file yard.png < painted.md
  yardr mail --with yardmaster@north`,
		Args: func(c *cobra.Command, args []string) error {
			if with != "" {
				return cobra.NoArgs(c, args)
			}
			if len(files) > store.MaxMailFiles {
				return fmt.Errorf("mail carries at most %d files, not %d", store.MaxMailFiles, len(files))
			}
			if len(files) > 0 {
				return cobra.RangeArgs(1, 2)(c, args)
			}
			return cobra.ExactArgs(2)(c, args)
		},
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			st, err := a.store(ctx)
			if err != nil {
				return err
			}
			if err := a.unknownLogin(ctx, st); err != nil {
				return err
			}
			if with != "" {
				return a.conversation(c.OutOrStdout(), ctx, st, with, after)
			}
			text := ""
			if len(args) > 1 {
				text = args[1]
			}
			if text == "-" {
				b, err := io.ReadAll(c.InOrStdin())
				if err != nil {
					return err
				}
				text = strings.TrimRight(string(b), "\n")
			}
			to, peer, outside := strings.Cut(args[0], "@")
			if outside && len(files) > 0 {
				// Before any is put: what is refused leaves nothing behind.
				p, err := st.GetPeer(ctx, peer)
				if err != nil {
					return err
				}
				if err := p.SendsFiles(); err != nil {
					return err
				}
			}
			if !outside {
				if _, err := st.GetUser(ctx, to); err == nil {
					if err := a.mayMailUser(to); err != nil {
						return err
					}
				} else if !errors.Is(err, store.ErrNotFound) {
					return err
				}
				// Before any file is put: a mail refused for its sender leaves nothing.
				from, foreign, err := a.mailForeign(ctx, st)
				if err != nil {
					return err
				}
				refs, err := putFiles(ctx, st, files)
				if err != nil {
					return err
				}
				if foreign {
					err = store.MailTextFrom(st, ctx, to, text, a.who(), from, blobFiles(refs)...)
				} else {
					err = store.MailText(st, ctx, to, text, a.who(), blobFiles(refs)...)
				}
				if err != nil {
					return err
				}
				mails := len(store.SplitMail(text))
				sent := map[string]any{"to": to, "sent": "ok"}
				if mails > 1 {
					sent["mails"] = mails
				}
				if len(refs) > 0 {
					sent["files"] = refs
				}
				return a.out(c.OutOrStdout(), sent, func(w io.Writer) {
					if mails > 1 {
						fmt.Fprintf(w, "%d mails sent to %s\n", mails, to)
					} else {
						fmt.Fprintln(w, "mail sent to "+to)
					}
					if line := store.NoteFilesLine(a.mailHome(), refs); line != "" {
						fmt.Fprintln(w, line)
					}
				})
			}
			refs, err := putFiles(ctx, st, files)
			if err != nil {
				return err
			}
			m, err := st.MailPeer(ctx, to, peer, text, a.who(), blobFiles(refs)...)
			if err != nil {
				return err
			}
			queued := map[string]any{"to": to, "peer": peer, "id": m.ID, "sent": "queued"}
			if len(refs) > 0 {
				queued["files"] = refs
			}
			return a.out(c.OutOrStdout(), queued, func(w io.Writer) {
				fmt.Fprintf(w, "mail for %s@%s queued; the daemon delivers it, and yardr peer show %s lists it while it waits\n", to, peer, peer)
				if line := store.NoteFilesLine(a.mailHome(), refs); line != "" {
					fmt.Fprintln(w, line)
				}
			})
		},
	}
	cmd.Flags().StringVar(&with, "with", "", "print the mail between you and this crew member or user, or <name>@<peer>, instead of sending")
	cmd.Flags().Uint64Var(&after, "after", 0, "with --with: only mail newer than this seq")
	cmd.Flags().StringArrayVar(&files, "file", nil, "a file the mail carries (repeatable)")
	return cmd
}

// mailHome is the yard's home where mail is shown: the files it carries
// are named by their paths in it (store.NoteFilesLine).
func (a *app) mailHome() string {
	cfg, err := a.config()
	if err != nil {
		return ""
	}
	return cfg.Home
}

// putFiles puts each file of paths in the yard as a blob, in order: what
// mail or a note is then sent with. Each carries the path's base name as
// its name. The first the yard refuses ends it, named.
func putFiles(ctx context.Context, st store.API, paths []string) ([]store.BlobRef, error) {
	var out []store.BlobRef
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		ref, err := st.PutBlob(ctx, data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ref.Name = filepath.Base(path)
		out = append(out, *ref)
	}
	return out, nil
}

// mailForeign says whether mail this run sends into its home is from another
// yard: its actor is neither a crew member nor a user of the home, and the run
// started in another home (--home), whose yard is the sender. Then from is
// the name the home has for that yard's link, which the yard's own key
// identifies. A home with no link to the sending yard refuses, as no reply
// could reach the sender from it.
func (a *app) mailForeign(ctx context.Context, st store.API) (from string, foreign bool, err error) {
	actor := a.who()
	if a.ownHome == "" || a.ownHome == a.home() {
		return "", false, nil
	}
	if _, err := st.GetCrew(ctx, actor); err == nil {
		return "", false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", false, err
	}
	if _, err := st.GetUser(ctx, actor); err == nil {
		return "", false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", false, err
	}
	yard, _, err := yardkey.Ensure((&config.Config{Home: a.ownHome}).PeerKey())
	if err != nil {
		return "", false, err
	}
	peers, err := st.Peers(ctx)
	if err != nil {
		return "", false, err
	}
	key := store.Public(yard)
	for _, p := range peers {
		if p.Key == key {
			return p.Name, true, nil
		}
	}
	return "", false, fmt.Errorf("mail from %s, of the yard at %s, into a home with no link to that yard: no reply could reach its sender from here. "+
		"Link the two yards with yardr peer add (yardr peer list shows this yard's links)", actor, a.ownHome)
}
