.PHONY: test todo-gc setup-minigo storybook

## setup-minigo: clone minigo (unstable, not yet published) into third_party/
setup-minigo:
	@if [ ! -d third_party/minigo ]; then \
		git clone --depth 1 https://github.com/podhmo/minigo third_party/minigo; \
	fi

## storybook: serve the internal/web stories on :6006 (needs `make setup-minigo` first)
storybook: setup-minigo
	cd cmd/storybook && go run . -http 127.0.0.1:6006 ../../internal/web

## test: run all tests
test:
	go test ./...

## todo-gc: move [x] entries from TODO.md to TODO-archive.md, one line per item
todo-gc:
	cd tools/todo-gc && go run . -w ../..
