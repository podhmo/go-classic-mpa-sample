// Command issues is the gh-like CLI client for the issue tracker —
// a separate binary from the server (docs/sketch/ja/cli-and-web-api.md,
// 案2: cmd/issues 別バイナリ). Distributing a gh-style client must not
// require shipping the server with it, so unlike `init` these commands
// are not dispatched through the server binary's argv.
//
//	issues issue list|view|create|edit|close|reopen|delete
//	issues comment create N --body B
//	issues label list
//	issues discussion list|view|create|edit|delete
//	issues discussion comment create N --body B [--thread T]
//	issues discussion answer N --thread T [--unmark]
//	issues auth token --user NAME | token list | token revoke  (local mode only)
//	issues api <path> [-X METHOD] [-f k=v]... [-F k=v]...       (remote mode only)
//	issues completion bash|zsh|fish
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
