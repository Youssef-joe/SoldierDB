.PHONY: test run fmt

GOCACHE := $(CURDIR)/.gocache

test:
	GOCACHE="$(GOCACHE)" go test ./...

run:
	GOCACHE="$(GOCACHE)" go run ./cmd/myowndb

fmt:
	gofmt -w ./cmd ./internal
