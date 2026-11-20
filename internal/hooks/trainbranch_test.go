package hooks

import (
	"context"
	"strings"
	"testing"
)

// branchStore answers the one question deriveTrain asks of the store: which
// bead records a branch name.
type branchStore struct {
	Store
	records map[string]string
}

func (s branchStore) BeadOfBranch(_ context.Context, name string) (string, error) {
	return s.records[name], nil
}

// A webhook's head branch names its train: by the yard's own yard/<train>
// name, which needs no record, or by the name a public depot's train records,
// which only the record can tell from any other branch.
func TestDeriveTrainReadsTheRecordedBranch(t *testing.T) {
	t.Parallel()
	rc := &Receiver{st: branchStore{records: map[string]string{"feat/checkout-totals": "yardr-x1"}}}
	for head, want := range map[string]string{
		"yard/yardr-t1":        "yardr-t1",
		"feat/checkout-totals": "yardr-x1",
		"main":                 "",
		"fix/unrecorded":       "",
	} {
		env := &Envelope{Fields: map[string][]string{"head_branch": {head}}}
		rc.deriveTrain(context.Background(), env)
		if got := strings.Join(env.Fields["train"], ","); got != want {
			t.Errorf("head %q: train %q, want %q", head, got, want)
		}
	}
	// A train the envelope already names keeps it.
	env := &Envelope{Fields: map[string][]string{"head_branch": {"feat/checkout-totals"}, "train": {"given"}}}
	rc.deriveTrain(context.Background(), env)
	if got := env.Fields["train"]; len(got) != 1 || got[0] != "given" {
		t.Errorf("a named train became %v", got)
	}
}
