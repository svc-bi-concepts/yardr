//go:build !race

// The two 32 MiB legs of TestMaxFileCrossesSealed, kept out of the race
// build (yardr-8ern, docs/testing.md R4). Under -race each leg holds a 32 MiB
// file through two yards, and that detector cost, not the product's, is what
// put this package over its 60 s budget (docs/testing.md R1). The bytes cross
// the same code with or without the detector, so these legs run without it:
// the mirror's plain step runs TestMaxFileCrossesSealed on every landed tree,
// these included (.github/workflows/mirror.yml). The too_large leg, the 413
// leg and every small-file test stay under -race in files_test.go.

package yards_test

import (
	"bytes"
	"context"
	"os"
	"testing"

	"yardr.dev/yardr/internal/store"
)

// A file of the largest size the setting can allow (32 MiB) crosses sealed,
// byte-identical, both posted and pulled. The two legs run one after the
// other, not beside one another: each holds a 32 MiB file through two yards.
func TestMaxFileCrossesSealedPostedAndPulled(t *testing.T) {
	ctx := context.Background()
	data, hash := maxFile()

	t.Run("posted", func(t *testing.T) {
		south, north, toNorth := fileYards(t, true, mailAndFile, mailAndFile)
		setMaxCaps(t, south)
		setMaxCaps(t, north)
		ref, err := south.api.PutBlob(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := south.api.MailPeer(ctx, "yardmaster", "north", "the big shed", "jo", store.BlobFile{Hash: ref.Hash, Name: "big.bin"}); err != nil {
			t.Fatal(err)
		}
		south.post(t, toNorth)
		got, err := os.ReadFile(north.own.BlobPath(hash))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("posted: north's blob = %d bytes, %v; want the 32 MiB file byte-identical", len(got), err)
		}
	})

	// Pulled: north cannot be called, so it asks south, and the file
	// crosses in the sealed page.
	t.Run("pulled", func(t *testing.T) {
		south, north, _ := fileYards(t, false, mailAndFile, mailAndFile)
		setMaxCaps(t, south)
		setMaxCaps(t, north)
		ref, err := south.api.PutBlob(ctx, data)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := south.api.MailPeer(ctx, "yardmaster", "north", "pulled", "jo", store.BlobFile{Hash: ref.Hash, Name: "big.bin"}); err != nil {
			t.Fatal(err)
		}
		toSouth, err := north.api.GetPeer(ctx, "south")
		if err != nil {
			t.Fatal(err)
		}
		batch, err := north.caller().Pull(ctx, *toSouth, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := north.api.PeerPulled(ctx, "south", batch); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(north.own.BlobPath(hash))
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("pulled: north's blob = %d bytes, %v; want the 32 MiB file byte-identical", len(got), err)
		}
	})
}
