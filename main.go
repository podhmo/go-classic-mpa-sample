// Command go-classic-mpa-sample runs the issue tracker web app for
// development (`go run .`). The distributable server binary is
// cmd/zatudan; both call the same internal/app bootstrap.
//
// The gh-like CLI is a separate binary, cmd/zatu — clients of the web
// API should not have the server embedded in their tool.
package main

import "github.com/podhmo/go-classic-mpa-sample/internal/app"

func main() {
	app.Run()
}
