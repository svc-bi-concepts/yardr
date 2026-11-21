package store

import (
	"context"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// BranchOfBead is the git branch bead b is worked on: the name it records
// (Bead.Branch), set when it was made in a public depot, else Branch(b.ID).
// Whoever holds the bead asks this; a bare handle reads the bead first
// (branchIn, Store.branchOf).
func BranchOfBead(b Bead) string {
	if b.Branch != "" {
		return b.Branch
	}
	return Branch(b.ID)
}

// branchIn is BranchOfBead for the bead ref names, read in tx. A ref that
// names no bead gets the yard's name for it, which is what a branch of a bead
// that is gone has always been called.
func branchIn(tx *bolt.Tx, ref string) string {
	row, err := loadBead(tx, ref)
	if err != nil {
		return Branch(ref)
	}
	return BranchOfBead(row.Bead)
}

// branchOf is branchIn outside a transaction.
func (s *Store) branchOf(ref string) string {
	out := Branch(ref)
	_ = s.read(func(tx *bolt.Tx) error {
		out = branchIn(tx, ref)
		return nil
	})
	return out
}

// beadOfBranch is the handle of the bead that records name as its branch, in
// tx, or "" when no bead does. A branch named the yard's way (yard/<handle>)
// records nothing and is not found here: a caller that takes that form reads
// the handle from the name first.
func beadOfBranch(tx *bolt.Tx, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	found := ""
	err := each(tx, bBeads, "", func(_ []byte, r *beadRow) error {
		if r.Branch == name {
			found = r.ID
		}
		return nil
	})
	return found, err
}

// BeadOfBranch is the handle of the bead whose recorded branch is name, or ""
// when no bead records it. A branch a bead has by the yard's own name is not
// found here, since it records nothing: a caller that accepts yard/<handle>
// takes that form first.
func (s *Store) BeadOfBranch(ctx context.Context, name string) (string, error) {
	var out string
	err := s.read(func(tx *bolt.Tx) error {
		var err error
		out, err = beadOfBranch(tx, name)
		return err
	})
	return out, err
}

// publicBranch names the branch of a bead made in a public depot:
// <fix|feat>/<slug>, with no bead id in it, since a public depot's branches
// and commits are public. A bug is fix, anything else feat. The slug is the
// title's words in lower case, joined by dashes, and taken says whether a
// bead already records a name; a second bead with the same title gets -2,
// -3 and so on.
func publicBranch(typ, title string, taken func(string) bool) string {
	prefix := "feat"
	if typ == "bug" {
		prefix = "fix"
	}
	slug := branchSlug(title)
	name := prefix + "/" + slug
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s/%s-%d", prefix, slug, n)
	}
	return name
}

// branchSlugMax bounds the slug of a branch name, so a long title makes a
// name of reasonable length.
const branchSlugMax = 40

// branchSlug is title as a branch name's slug: its ASCII letters and digits in
// lower case, each run of anything else one dash, with no dash at either end.
// A title with none of them is "bead", so the name is never bare.
func branchSlug(title string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.TrimSuffix(b.String(), "-")
	if len(slug) > branchSlugMax {
		slug = strings.TrimSuffix(slug[:branchSlugMax], "-")
	}
	if slug == "" {
		return "bead"
	}
	return slug
}

// publicBranchFor is the branch a bead made in depot with typ and title
// records: a public depot's name (publicBranch), or "" for a private depot,
// a dir depot's bead and a bead with no depot, which keep the yard's name. It
// is taken once, at the bead's creation, so a title edited later leaves the
// branch as it is.
func (w *wtx) publicBranchFor(depot, typ, title string) (string, error) {
	if depot == "" {
		return "", nil
	}
	var d Depot
	ok, err := get(w.Tx, bDepots, depot, &d)
	if err != nil {
		return "", err
	}
	if !ok || depotVisibility(d.Visibility) != DepotPublic {
		return "", nil
	}
	taken := map[string]bool{}
	err = each(w.Tx, bBeads, "", func(_ []byte, r *beadRow) error {
		if r.Branch != "" {
			taken[r.Branch] = true
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return publicBranch(typ, title, func(name string) bool { return taken[name] }), nil
}
