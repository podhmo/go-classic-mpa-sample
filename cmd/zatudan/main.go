// Command zatudan is the distributable server binary of the issue
// tracker web app — a classic multi-page application. It shares its
// bootstrap with the repo-root main (`go run .`), which stays the
// development entrypoint.
//
// The "init" subcommand (or the -init flag) prepares the database file
// and schema, refreshes planner statistics, prints a report, and exits
// without starting the server.
//
//	zatudan -addr 127.0.0.1:8080 -db zatudan.db
//	zatudan -db zatudan.db init
//
// The gh-like CLI is a separate binary, cmd/zatu.
package main

import "github.com/podhmo/go-classic-mpa-sample/internal/app"

func main() {
	app.Run()
}
