.PHONY: dev server web build test

GO_TAGS ?= sqlite_fts5

dev:
	npm --prefix web run dev

server:
	go run -tags "$(GO_TAGS)" ./cmd/firescribe-server

web:
	npm --prefix web run build

build: web
	go build -tags "$(GO_TAGS)" ./cmd/firescribe-server

test:
	go test -tags "$(GO_TAGS)" ./...
