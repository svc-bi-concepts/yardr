package store

import "context"

// DepotVisibilityEvent is the event the daily visibility check writes when a
// git depot's recorded visibility is not the host's.
const DepotVisibilityEvent = "depot_visibility"

// ReasonVisibility is the delivery reason of that event in the yardmaster's
// inbox, as ReasonTimer is a timer's.
const ReasonVisibility = "depot visibility"

// TellDepotVisibility records that depot is recorded as recorded and the host
// says actual, and delivers the event to the yardmaster's inbox as a fired
// timer's is delivered to its crew member. It changes no depot: the recorded
// value is the operator's to set, and the check only says where it differs.
func (s *Store) TellDepotVisibility(ctx context.Context, depot, recorded, actual string) error {
	return s.write(func(w *wtx) error {
		data := map[string]any{"depot": depot, "recorded": recorded, "actual": actual}
		// The yardmaster is named by its setting, and the default where
		// that is unset, as the delegation's mail is sent to it.
		ym, err := yardmasterIn(w.Tx)
		if err != nil {
			return err
		}
		data["deliveries"] = map[string]any{ym: ReasonVisibility}
		return w.event("", DepotVisibilityEvent, "yard", data)
	})
}
