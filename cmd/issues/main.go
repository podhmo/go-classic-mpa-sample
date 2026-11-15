// Command issues is the gh-like CLI client for the issue tracker —
// a separate binary from the server (docs/sketch/ja/cli-and-web-api.md,
// 案2: cmd/issues 別バイナリ). Distributing a gh-style client must not
// require shipping the server with it, so unlike `init` these commands
// are not dispatched through the server binary's argv.
//
//	issues issue list|view|create|edit|close|reopen|delete
//	issues comment create N --body B
//	issues label list
//	issues auth token --user NAME     (local mode only)
//	issues api <path>                 (remote mode only)
//
// Backend selection: --server/--token (or ISSUES_SERVER/ISSUES_TOKEN) for
// remote mode over /api/v1; otherwise -db/ISSUES_DB opens SQLite directly.
package main

import (
	"context"
	"os"

	"github.com/podhmo/go-classic-mpa-sample/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), cli.Options{Args: os.Args[1:]}))
}
