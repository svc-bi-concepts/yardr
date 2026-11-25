package dispatch

import (
	"context"
	"strings"
	"time"
)

// draft is text seen in an agent's input on a pass that had something to
// type there.
type draft struct {
	since time.Time // when it was first seen
	said  bool      // the nudge_held event of this wait is logged
	drawn bool      // Ctrl-L was sent once during this held spell
	// unread: the last pass could not read the input. unreadSaid: the
	// nudge_held for that is logged. Both are cleared by a read that tells.
	unread, unreadSaid bool
}

// hold is what the one nudge_held event of a wait has to say, on the pass
// that is to say it (see inputHeld): the text has been in the input for
// waited, the input is a dialog of the harness, known by its word, with
// line its first, or the pane shows the harness's login screen, with login
// its line, or the pane has stayed unreadable as an input (unreadable). The
// zero hold says nothing.
type hold struct {
	waited       time.Duration
	dialog, line string
	login        string
	unreadable   bool
	composer     string // why the input reads as text (ComposerReader), "" when not told
}

func (h hold) tells() bool {
	return h.waited > 0 || h.dialog != "" || h.login != "" || h.unreadable
}

type ownNudge struct {
	text string
	at   time.Time
}

// inputHeld reports whether the input of the session of handle holds text,
// so that nothing may be typed there on this pass; key names the session
// in the dispatcher's memory. why tells on the one pass of a wait that is
// to say so: the text has then been there for DraftWait, or it is a dialog
// of the harness, which is said at once.
//
// Found live (yardr-ps05): a person was typing in the yardmaster's pane when
// the yardmaster was nudged about its inbox. The nudge was typed after their
// words and its Enter sent both as one message; what they had still meant to
// write was lost. Text in an input is someone's, so yardr types nothing after
// it and looks again on the next pass: what it had to say stays due.
//
// Text that stays is a draft someone left. It is never cleared and never
// typed after, however long it sits: both would send or destroy words a
// person wrote, to deliver a line that is still true and still due once
// they are gone. What a long wait costs is that the agent is not told, so
// after DraftWait one nudge_held event says who is waiting on whose input,
// once per wait.
//
// The one text yardr sends is its own, alone in the input because its Enter
// was not taken: that is ownInput's, which looks before anything is typed.
//
// Found live (yardr-vsen): what holds the input may be the harness's own
// question, drawn where the input is. One asks an agent that sat idle past
// its model's cache how to go on, and marks the answer Enter would choose
// as an input is marked (the dialog cache_menu of harnesses/kimi.yaml). Nobody is typing there, so nobody will
// clear it, and the agent sits until someone answers: that is said at
// once, and DraftWait is not waited for (see dialogAt).
//
// Found live (yardr-m519): a harness that is not logged in draws its login
// screen where its input is, and Claude Code's menu of methods carries the
// mark of the input before its selected option. Read as an input it is
// text someone left, and read for its screen it is up, so the first prompt
// was typed into the menu; with another status the daemon waited for a
// human. A pane at a login screen is its own state (see loginAt): nothing
// is typed there, and the event names the login, not the text.
//
// A runner that cannot look, or a harness whose input it cannot tell, says
// nothing: yardr then types as it did before it looked.
func (d *Dispatcher) inputHeld(ctx context.Context, r Runner, key string, handle map[string]any) (held bool, why hold) {
	if at, why := d.loginAt(ctx, r, key, handle); at {
		return true, why
	}
	read := func() (text string, known bool, composer string, err error, ok bool) {
		switch rd := r.(type) {
		case ComposerReader:
			text, known, composer, err = rd.InputComposer(ctx, handle)
			return text, known, composer, err, true
		case InputReader:
			text, known, err = rd.InputText(ctx, handle)
			return text, known, "", err, true
		}
		return "", false, "", nil, false
	}
	done := askTimer(ctx, askPane)
	text, known, composer, err, ok := read()
	if !ok {
		done()
		return false, hold{}
	}
	done()
	if err != nil || !known {
		// A broken or incomplete pane read says nothing about whether the
		// user's draft is still there. Redraw once, then retain the wait and
		// check again soon if the new read is still inconclusive.
		redrawer, canRedraw := r.(InputRedrawer)
		if !canRedraw {
			return false, hold{}
		}
		w := d.drafts[key]
		if w == nil || !w.drawn {
			marked, rerr := redrawer.RedrawInput(ctx, handle)
			if !marked {
				return false, hold{}
			}
			if w == nil {
				w = &draft{}
				d.drafts[key] = w
			}
			w.drawn = true
			if rerr == nil && waitContext(ctx, heldRedrawWait) == nil {
				done := askTimer(ctx, askPane)
				text, known, _, err, _ = read()
				done()
				if err == nil && known && strings.TrimSpace(text) == "" {
					delete(d.drafts, key)
					delete(d.dialogs, key)
					return false, hold{}
				}
				if err == nil && known && w.since.IsZero() {
					w.since = d.now()
				}
			}
		}
		d.lookAt(d.now().Add(heldInputRetry))
		if err == nil && known {
			// Only the redraw's re-read can be known here (the first read
			// failed or was not known), and it has text, since an empty one
			// returned above. The pane reads as an input again, so this pass
			// is not an unreadable one: a later unreadable read is a new spell.
			w.unread, w.unreadSaid = false, false
			return true, hold{}
		}
		// Held and not typed, as before. Said once per wait, and only when
		// a second pass reads it unreadable as well: one garbled read is
		// often a repaint in progress, not the pane's state.
		if !w.unread {
			w.unread = true
			return true, hold{}
		}
		if w.unreadSaid {
			return true, hold{}
		}
		w.unreadSaid = true
		return true, hold{unreadable: true}
	}
	if strings.TrimSpace(text) == "" {
		delete(d.drafts, key)
		delete(d.dialogs, key) // no mark, or nothing after it: no dialog is there
		return false, hold{}
	}
	now := d.now()
	w := d.drafts[key]
	if w == nil {
		w = &draft{since: now}
		d.drafts[key] = w
	} else if w.since.IsZero() {
		w.since = now
	}
	// The input read, so a later unreadable spell is a new one to say.
	w.unread, w.unreadSaid = false, false
	if !w.drawn {
		if redrawer, ok := r.(InputRedrawer); ok {
			w.drawn = true
			if _, err := redrawer.RedrawInput(ctx, handle); err == nil {
				// Ctrl-L only redraws the same pane. A short bounded settle
				// lets the harness repaint before a fresh, authoritative read.
				if waitContext(ctx, heldRedrawWait) == nil {
					done := askTimer(ctx, askPane)
					text, known, composer, err, _ = read()
					done()
					if err == nil && known && strings.TrimSpace(text) == "" {
						delete(d.drafts, key)
						delete(d.dialogs, key)
						return false, hold{}
					}
				}
			}
		}
	}
	d.lookAt(now.Add(heldInputRetry))
	if at, why := d.dialogAt(ctx, r, key, handle); at {
		w.said = true // the dialog's event is this wait's
		return true, why
	}
	if !w.said && now.Sub(w.since) >= d.DraftWait {
		w.said = true
		return true, hold{waited: now.Sub(w.since), composer: composer}
	}
	return true, hold{}
}

const (
	heldInputRetry = 3 * time.Minute
	heldRedrawWait = 250 * time.Millisecond
)

func waitContext(ctx context.Context, delay time.Duration) error {
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
