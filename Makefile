.PHONY: test todo-gc

## test: run all tests
test:
	go test ./...

## todo-gc: move [x] entries from TODO.md to TODO-archive.md, one line per item
todo-gc:
	cd tools/todo-gc && go run . -w ../..
