module github.com/podhmo/go-classic-mpa-sample/cmd/storybook

go 1.27.3

require (
	github.com/podhmo/minigo v0.0.0
	github.com/yuin/goldmark-alert v1.0.2
	github.com/yuin/goldmark/v2 v2.1.16
)

require golang.org/x/mod v0.42.0 // indirect

replace github.com/podhmo/go-classic-mpa-sample => ../../

// minigo is unstable and not yet published; use a local clone.
// `make setup-minigo` clones it into third_party/minigo.
replace github.com/podhmo/minigo => ../../third_party/minigo
