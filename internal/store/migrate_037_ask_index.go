package store

import (
	"encoding/json"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// migrateAskIndex fills, once, the index of asks by the person they are
// addressed to (bAskTo, from schema 37): every ask note a store already holds
// is put under its person, as indexAsk puts a new one. Until then a queue
// read every bead's notes to find the asks of one person, which is what made
// what_waits_for_me slow on a yard with a large history. A build before it
// never opens the store: checkSchema refuses a store newer than the build
// knows before any migration or bucket write, so none reads a queue without
// the index it would then miss asks from.
func migrateAskIndex(tx *bolt.Tx, from uint64) error {
	if from >= 37 {
		return nil
	}
	c := tx.Bucket(bNotes).Cursor()
	for k, raw := c.First(); k != nil; k, raw = c.Next() {
		var n Note
		if err := json.Unmarshal(raw, &n); err != nil {
			return fmt.Errorf("%s/%q: %w", bNotes, k, err)
		}
		if n.Kind != NoteAsk {
			continue
		}
		uuid, _, _ := strings.Cut(string(k), "\x00")
		if err := indexAsk(tx, uuid, n); err != nil {
			return err
		}
	}
	return nil
}
