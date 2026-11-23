package pack_test

import (
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"yardr.dev/yardr/internal/pack"
	"yardr.dev/yardr/internal/source"
	"yardr.dev/yardr/internal/store"
	"yardr.dev/yardr/packs"
)

// slackPack is the embedded slack pack, loaded as an import of a yard would
// load it.
func slackPack(t *testing.T) *pack.Pack {
	t.Helper()
	sub, err := fs.Sub(packs.FS, "slack")
	if err != nil {
		t.Fatal(err)
	}
	p, err := pack.Load(sub, packs.Embedded)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// slackDeclaration is the declaration of the source the slack pack made.
func slackDeclaration(t *testing.T, st store.API) (*source.Declaration, *store.Source) {
	t.Helper()
	src, err := st.GetSource(ctx, "slack")
	if err != nil {
		t.Fatal(err)
	}
	d, err := src.Declaration()
	if err != nil {
		t.Fatal(err)
	}
	return d, src
}

// The slack pack declares the vault key its token is minted from, as the
// google-mail pack declares its account: applying it gives the source the
// mint input, and applying it again keeps it.
func TestSlackPackMintInputSurvivesApply(t *testing.T) {
	st, o := yard(t)
	// A source pack is imported into the yard, not into a depot.
	o.Depot = ""
	applySlack := func() {
		t.Helper()
		if _, err := pack.Apply(ctx, st, build(t, st, slackPack(t), o), pack.ApplyOptions{Actor: "me"}); err != nil {
			t.Fatal(err)
		}
	}

	applySlack()
	d, _ := slackDeclaration(t, st)
	if d.Credential == nil || !slices.Equal(d.Credential.Mint, []string{"SLACK_VAULT_KEY"}) || !slices.Equal(d.Credential.NotSecret, []string{"SLACK_VAULT_KEY"}) {
		t.Fatalf("after apply the credential is %+v", d.Credential)
	}
	// The pack brings no command: the mint runs the operator's setting, and
	// a yard that never set one has none.
	if len(d.Credential.MintCommand) != 0 {
		t.Fatalf("the pack brings a mint_command %q", d.Credential.MintCommand)
	}

	// Applied again the plan changes nothing about the source, and the mint
	// input is still there.
	plan := build(t, st, slackPack(t), o)
	if got := find(t, plan, "source", "slack"); got.Action != pack.Unchanged {
		t.Fatalf("a second apply would %s the source", got.Action)
	}
	applySlack()
	if d, _ = slackDeclaration(t, st); !slices.Equal(d.Credential.Mint, []string{"SLACK_VAULT_KEY"}) {
		t.Fatalf("after a second apply the mint is %q", d.Credential.Mint)
	}
}

// A yard with no mint_command on the slack source, and only its static bot
// token in the env file, reads that token and calls without a mint: the
// declared mint is not the one that will be used, so it does not stand in
// the way.
func TestSlackPackWithoutMintCommandCallsWithTheStaticToken(t *testing.T) {
	st, o := yard(t)
	o.Depot = ""
	if _, err := pack.Apply(ctx, st, build(t, st, slackPack(t), o), pack.ApplyOptions{Actor: "me"}); err != nil {
		t.Fatal(err)
	}
	d, _ := slackDeclaration(t, st)
	envFile := filepath.Join(t.TempDir(), "slack.env")
	if _, _, err := store.WriteEnvFile(envFile, map[string]string{"SLACK_BOT_TOKEN": "static-test-value"}); err != nil {
		t.Fatal(err)
	}

	state, err := d.CredentialState(envFile, nil, store.ReadEnvFileEntries)
	if err != nil {
		t.Fatalf("credential state without a mint command: %v", err)
	}
	token, _, err := d.ReadCredential(ctx, envFile, nil, source.Read, store.ReadEnvFileEntries)
	if err != nil || token != "static-test-value" {
		t.Fatalf("the call read %q (%q), %v", token, state, err)
	}
}
