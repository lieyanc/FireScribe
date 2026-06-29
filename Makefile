.PHONY: dev server web stage build test

GO_TAGS ?= sqlite_fts5

dev:
	npm --prefix web run dev

server:
	go run -tags "$(GO_TAGS)" ./cmd/firescribe-server

web:
	npm --prefix web run build

stage: web
	npm run stage:web

build: stage
	go build -tags "$(GO_TAGS)" -o bin/firescribe ./cmd/firescribe-server

test:
	go test -tags "$(GO_TAGS)" ./...
