// `issues api <path>` — the raw-API escape hatch (gh api equivalent),
// remote mode only.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// issues api <path> — GET the path against the configured server and print
// the body verbatim. Non-2xx responses print the body to stderr and exit 1.
func apiCmd(ctx context.Context, o Options, args []string) error {
	fs := flag.NewFlagSet("api", flag.ContinueOnError)
	bf := o.addBackendFlags(fs)
	rest, err := parseLeaf(fs, args, 1)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return usageError{msg: "usage: issues api <path>"}
	}
	if bf.server == "" {
		return usageError{msg: "issues api requires --server (or ISSUES_SERVER)"}
	}

	// Paths resolve under /api/v1 like gh api resolves under its API host,
	// so both `api issues` and `api /issues` hit the JSON endpoint.
	path := strings.TrimPrefix(rest[0], "/api/v1/")
	path = "/api/v1/" + strings.TrimPrefix(path, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(bf.server, "/")+path, nil)
	if err != nil {
		return err
	}
	if bf.token != "" {
		req.Header.Set("Authorization", "Bearer "+bf.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", req.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		fmt.Fprintf(o.Stderr, "%s %s → %s\n%s", req.Method, req.URL, resp.Status, body)
		return fmt.Errorf("API returned %s", resp.Status)
	}
	fmt.Fprint(o.Stdout, string(body))
	return nil
}
