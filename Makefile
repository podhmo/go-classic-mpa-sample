.PHONY: test test-storybook todo-gc storybook storybook-watch

## test: run all tests
test:
	go test ./...

## test-storybook: run storybook tests without Go's test cache (stories are interpreted at runtime)
test-storybook:
	go test -count=1 ./cmd/storybook

# CLI の E2E: init → register → auth token → serve → zatu/リモート動作確認
test-e2e:
	go test -tags=e2e -v -run 'TestCLIE2E' ./internal/e2etests/

## storybook: serve the internal/web stories on :6006
storybook:
	go run ./cmd/storybook -http 127.0.0.1:6006 ./internal/web

## storybook-watch: storybook + live reload (open pages refresh on file changes)
storybook-watch:
	go run ./cmd/storybook -http 127.0.0.1:6006 -watch ./internal/web

## todo-gc: move [x] entries from TODO.md to TODO-archive.md, one line per item
todo-gc:
	cd tools/todo-gc && go run . -w ../..
