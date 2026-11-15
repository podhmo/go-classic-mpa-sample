// `issues auth token` — per-user API token issuance (local mode only).
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/podhmo/go-classic-mpa-sample/internal/store"
)

// issues auth token --user NAME
//
// Issuing a token writes the api_tokens row directly to the database, so
// it only works in local mode — the point is to mint a credential without
// exposing password verification over HTTP (cli-and-web-api.md「認証」節).
func authCmd(ctx context.Context, o Options, args []string) error {
	if len(args) == 0 || args[0] != "token" {
		return usageError{msg: "usage: issues auth token --user NAME"}
	}
	fs := flag.NewFlagSet("auth token", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	user := fs.String("user", "", "login name of the user to issue a token for (required)")
	if _, err := parseLeaf(fs, args[1:], 0); err != nil {
		return err
	}
	if *user == "" {
		return usageError{msg: "--user is required"}
	}
	if bf.server != "" {
		return usageError{msg: "auth token is a local-mode command; it writes the database directly and ignores --server"}
	}

	st, err := o.OpenTokenStore(bf.dbPath())
	if err != nil {
		return err
	}
	defer st.Close()

	u, err := st.UserByName(ctx, *user)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no such user: %s", *user)
	}
	if err != nil {
		return err
	}
	token, err := st.CreateAPIToken(ctx, u.ID)
	if err != nil {
		return err
	}
	// The plaintext is shown once and never stored — keep stdout to the
	// bare token so it can be captured (`export ISSUES_TOKEN=$(...)`).
	fmt.Fprintln(o.Stdout, token)
	fmt.Fprintf(o.Stderr, "issued API token for user %q (shown once; store it safely)\n", u.Name)
	return nil
}
